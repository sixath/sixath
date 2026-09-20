/** Tool / datasource egress helpers (pure; used by ToolForm and unit tests). */

export type EgressMode = 'inherit' | 'off' | 'proxy'

export interface ProxyReference {
  kind: string
  id: string
  name?: string
}

export function toolSupportsEgress(toolType: string): boolean {
  return toolType === 'datasource' || toolType === 'mcp' || toolType === 'rca'
}

export function datasourceForbidsNamedProxy(dsType?: string): boolean {
  const t = (dsType || '').toLowerCase().trim()
  return t === 'hive'
}

function datasourceNeedsSocks5Proxy(dsType?: string): boolean {
  const t = (dsType || '').toLowerCase().trim()
  return t === 'mysql' || t === 'mongo' || t === 'mongodb'
}

export function toolAllowsNamedProxy(toolType: string, datasourceType?: string): boolean {
  if (!toolSupportsEgress(toolType)) return false
  if (toolType === 'datasource' && datasourceForbidsNamedProxy(datasourceType)) return false
  return true
}

export function normalizeEgressMode(mode?: string): EgressMode {
  switch ((mode || '').toLowerCase().trim()) {
    case 'off':
    case 'direct':
      return 'off'
    case 'proxy':
      return 'proxy'
    default:
      return 'inherit'
  }
}

/** Hive cannot use egress_mode=proxy; coerce to inherit before submit. */
export function coerceEgressMode(
  mode: string | undefined,
  opts: { toolType: string; datasourceType?: string },
): EgressMode {
  const next = normalizeEgressMode(mode)
  if (next === 'proxy' && !toolAllowsNamedProxy(opts.toolType, opts.datasourceType)) {
    return 'inherit'
  }
  return next
}

/** MySQL / MongoDB named-proxy picker only lists SOCKS5. Other types keep the full catalog. */
export function filterProxiesForTool<T extends { type: string }>(
  proxies: T[],
  opts: { toolType: string; datasourceType?: string },
): T[] {
  if (opts.toolType === 'datasource' && datasourceNeedsSocks5Proxy(opts.datasourceType)) {
    return proxies.filter((p) => p.type === 'socks5')
  }
  return proxies
}

/**
 * MySQL / MongoDB egress_mode=proxy requires a SOCKS5 proxy.
 * A stored HTTP id (or any id not in the socks5-filtered list) coerces to inherit.
 */
export function coerceMysqlNamedProxy(
  mode: EgressMode,
  proxyId: string | undefined,
  proxies: Array<{ id: string; type: string }>,
  datasourceType?: string,
): { mode: EgressMode; proxyId: string } {
  const id = (proxyId || '').trim()
  if (mode !== 'proxy') {
    return { mode, proxyId: '' }
  }
  if (!datasourceNeedsSocks5Proxy(datasourceType)) {
    return { mode, proxyId: id }
  }
  const allowed = filterProxiesForTool(proxies, { toolType: 'datasource', datasourceType })
  if (id && !allowed.some((p) => p.id === id)) {
    return { mode: 'inherit', proxyId: '' }
  }
  return { mode, proxyId: id }
}

export function parseProxyDeleteConflict(body: string): {
  references: ProxyReference[]
  truncated: boolean
  message: string
} | null {
  const raw = body.trim()
  if (!raw) return null
  try {
    const j = JSON.parse(raw) as {
      ret?: { reason?: string; message?: string; code?: number }
      references?: Array<{ kind?: string; id?: string; name?: string }>
      truncated?: boolean
    }
    const refs = Array.isArray(j.references) ? j.references : null
    const reason = j.ret?.reason
    if (reason !== 'PROXY_IN_USE' && j.ret?.code !== 409 && !refs) {
      return null
    }
    if (reason && reason !== 'PROXY_IN_USE' && !refs) {
      return null
    }
    return {
      references: (refs || []).map((r) => ({
        kind: String(r.kind || ''),
        id: String(r.id || ''),
        name: r.name ? String(r.name) : '',
      })),
      truncated: !!j.truncated,
      message: j.ret?.message || 'proxy is referenced by agents or tools',
    }
  } catch {
    return null
  }
}

export function formatProxyInUseMessage(refs: ProxyReference[], truncated?: boolean): string {
  const lines = refs.map((r) => {
    const kind = r.kind || 'resource'
    const label = r.name ? `${r.name}（${r.id}）` : r.id
    return `· ${kind}: ${label}`
  })
  let msg = '无法删除：仍被以下资源引用'
  if (lines.length) msg += `\n${lines.join('\n')}`
  if (truncated) msg += '\n还有更多引用未列出'
  return msg
}
