import { beforeEach, describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  AUTH_GATE_KEY,
  DEV_BOOTSTRAP_TOKEN,
  clearAuthGate,
  getApiToken,
  hasApiToken,
  logout,
  saveCredentials,
  setAuthGate,
} from '../src/api/auth.ts'

function installMemoryStorage() {
  const make = () => {
    const m = new Map<string, string>()
    return {
      getItem: (k: string) => (m.has(k) ? m.get(k)! : null),
      setItem: (k: string, v: string) => {
        m.set(k, String(v))
      },
      removeItem: (k: string) => {
        m.delete(k)
      },
      clear: () => m.clear(),
    }
  }
  ;(globalThis as any).localStorage = make()
  ;(globalThis as any).sessionStorage = make()
}

describe('auth gate', () => {
  beforeEach(() => {
    installMemoryStorage()
  })

  it('logout clears token and blocks env/bootstrap via gate', () => {
    saveCredentials('tok', 'org-1')
    assert.equal(getApiToken(), 'tok')
    logout()
    assert.equal(sessionStorage.getItem(AUTH_GATE_KEY), '1')
    assert.equal(getApiToken(), '')
    assert.equal(hasApiToken(), false)
  })

  it('saveCredentials clears gate and restores token', () => {
    setAuthGate()
    saveCredentials('new-tok', '')
    assert.equal(sessionStorage.getItem(AUTH_GATE_KEY), null)
    assert.equal(getApiToken(), 'new-tok')
  })

  it('exports DEV_BOOTSTRAP_TOKEN', () => {
    assert.equal(DEV_BOOTSTRAP_TOKEN, 'dev-bootstrap-token')
  })

  it('gate blocks even when localStorage empty (env/bootstrap would otherwise apply in DEV)', () => {
    setAuthGate()
    assert.equal(getApiToken(), '')
  })
})
