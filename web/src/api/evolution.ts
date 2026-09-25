import { request, checkRet, type BaseResponse } from './client'

export interface EvolutionProposal {
  id: string
  agent_id: string
  session_id: string
  turn_index: number
  signal_type: string
  confidence: number
  problem_summary: string
  proposed_content: string
  target_path: string
  target_action: string
  conflict: boolean
  conflict_detail?: string
  conflict_check_failed: boolean
  dedup_skipped: boolean
  status: string
  review_comment?: string
  created_at: string
  reviewed_at?: string
  reviewed_by?: string
}

export interface ListProposalsResponse {
  ret?: BaseResponse
  items: EvolutionProposal[]
  total: number
}

export const evolutionApi = {
  list: async (params?: { page?: number; page_size?: number; status?: string }) => {
    const q = new URLSearchParams()
    if (params?.page) q.set('page', String(params.page))
    if (params?.page_size) q.set('page_size', String(params.page_size))
    if (params?.status) q.set('status', params.status)
    const query = q.toString()
    const data = await request<ListProposalsResponse>(`/evolution/proposals${query ? '?' + query : ''}`)
    checkRet(data)
    return data
  },

  get: async (id: string) => {
    const data = await request<{ ret?: BaseResponse; item: EvolutionProposal }>(`/evolution/proposals/${id}`)
    checkRet(data)
    return data
  },

  approve: async (id: string) => {
    const data = await request<{ ret?: BaseResponse }>(`/evolution/proposals/${id}/approve`, { method: 'POST' })
    checkRet(data)
    return data
  },

  reject: async (id: string, comment?: string) => {
    const data = await request<{ ret?: BaseResponse }>(`/evolution/proposals/${id}/reject`, {
      method: 'POST',
      body: JSON.stringify({ comment: comment || '' }),
    })
    checkRet(data)
    return data
  },

  patch: async (id: string, proposedContent: string) => {
    const data = await request<{ ret?: BaseResponse }>(`/evolution/proposals/${id}`, {
      method: 'PATCH',
      body: JSON.stringify({ proposed_content: proposedContent }),
    })
    checkRet(data)
    return data
  },

  countPending: async () => {
    const data = await request<{ ret?: BaseResponse; count: number }>('/evolution/proposals/count')
    checkRet(data)
    return data
  },

  getConfig: async () => {
    const data = await request<{
      ret?: BaseResponse
      enabled: boolean
      config: Record<string, unknown> | null
    }>('/evolution/config')
    checkRet(data)
    return data
  },

  putConfig: async (body: { enabled: boolean }) => {
    const data = await request<{ ret?: BaseResponse; enabled: boolean }>('/evolution/config', {
      method: 'PUT',
      body: JSON.stringify(body),
    })
    checkRet(data)
    return data
  },
}