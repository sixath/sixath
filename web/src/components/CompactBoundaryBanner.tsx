import type { ChatMessage } from '../api/client'
import { formatCompactBoundaryTime } from '../utils/compactBoundary'
import './CompactBoundaryBanner.css'

export interface CompactBoundaryBannerProps {
  message: ChatMessage
  hiddenCount: number
  collapsed: boolean
  onToggle: () => void
}

export function CompactBoundaryBanner({
  message,
  hiddenCount,
  collapsed,
  onToggle,
}: CompactBoundaryBannerProps) {
  const timeLabel = formatCompactBoundaryTime(message.created_at)
  const toggleLabel = collapsed
    ? `展开上方 ${hiddenCount} 条历史`
    : `收起上方 ${hiddenCount} 条历史`

  return (
    <div className="compact-boundary-banner" role="separator" aria-label="会话上下文已压缩">
      <div className="compact-boundary-line" aria-hidden />
      <div className="compact-boundary-body">
        <div className="compact-boundary-title">
          上下文已压缩{timeLabel ? ` · ${timeLabel}` : ''}
        </div>
        <div className="compact-boundary-subtitle">摘要已写入会话状态，完整历史仍可查看</div>
        {hiddenCount > 0 ? (
          <button type="button" className="compact-boundary-toggle" onClick={onToggle}>
            <span>{toggleLabel}</span>
            <span aria-hidden>{collapsed ? '▸' : '▾'}</span>
          </button>
        ) : null}
      </div>
      <div className="compact-boundary-line" aria-hidden />
    </div>
  )
}
