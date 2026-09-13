export type SessionContinueApi = {
  listSessions: (
    agentId: string,
    opts?: { page?: number; pageSize?: number },
  ) => Promise<{ items: { id?: string }[] }>
  createSession: (agentId: string) => Promise<{ id: string }>
  listMessages: (sessionId: string) => Promise<{ items: unknown[]; next_cursor?: string }>
}

/** List API returns updated_at DESC; the first row is the most recently active session. */
export async function findLatestSessionId(
  agentId: string,
  api: Pick<SessionContinueApi, 'listSessions'>,
): Promise<string | undefined> {
  const listed = await api.listSessions(agentId, { page: 1, pageSize: 1 })
  const id = listed.items[0]?.id?.trim()
  return id || undefined
}

/**
 * Send / confirm without a selected session continues the latest one.
 * Creates a session only when this agent has none.
 */
export async function resolveSessionToContinue(
  agentId: string,
  currentSessionId: string | undefined,
  api: Pick<SessionContinueApi, 'listSessions' | 'createSession'>,
): Promise<{ id: string; created: boolean }> {
  const current = currentSessionId?.trim()
  if (current) return { id: current, created: false }
  const latest = await findLatestSessionId(agentId, api)
  if (latest) return { id: latest, created: false }
  const created = await api.createSession(agentId)
  return { id: created.id, created: true }
}

/**
 * Resolve the session to send into. When attaching to an existing session
 * (URL had no session id), load its transcript first so the composer does
 * not replace history with just the new message.
 * `history === null` means the caller already has the selected session's messages.
 */
export async function prepareSessionForSend(
  agentId: string,
  currentSessionId: string | undefined,
  api: SessionContinueApi,
): Promise<{ id: string; created: boolean; history: unknown[] | null; nextCursor: string }> {
  const current = currentSessionId?.trim()
  if (current) {
    return { id: current, created: false, history: null, nextCursor: '' }
  }
  const resolved = await resolveSessionToContinue(agentId, currentSessionId, api)
  if (resolved.created) {
    return { ...resolved, history: [], nextCursor: '' }
  }
  const res = await api.listMessages(resolved.id)
  return {
    ...resolved,
    history: res.items ?? [],
    nextCursor: res.next_cursor ?? '',
  }
}
