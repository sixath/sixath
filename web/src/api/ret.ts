/** proto3 json omitempty drops code:0; missing code after HTTP 200 means success. */
export type RetLike = { code?: number; message?: string; reason?: string }

export function isFailedRet(ret?: RetLike | null): boolean {
  if (ret == null) return false
  return (ret.code ?? 0) !== 0
}
