import type { ReactNode } from 'react'
import './ConfirmDialog.css'

export interface FormDialogProps {
  open: boolean
  title: string
  children: ReactNode
  confirmLabel?: string
  cancelLabel?: string
  loading?: boolean
  confirmDisabled?: boolean
  error?: string
  onConfirm: () => void
  onCancel: () => void
}

export function FormDialog({
  open,
  title,
  children,
  confirmLabel = '保存',
  cancelLabel = '取消',
  loading = false,
  confirmDisabled = false,
  error = '',
  onConfirm,
  onCancel,
}: FormDialogProps) {
  if (!open) return null
  return (
    <div className="confirm-dialog-backdrop" role="presentation">
      <div className="confirm-dialog form-dialog" role="dialog" aria-modal="true" aria-labelledby="form-dialog-title">
        <h2 id="form-dialog-title">{title}</h2>
        <div className="form-dialog__body">{children}</div>
        {error ? <div className="error form-dialog__error">{error}</div> : null}
        <div className="confirm-dialog-actions">
          <button type="button" className="btn btn-secondary" disabled={loading} onClick={onCancel}>
            {cancelLabel}
          </button>
          <button type="button" className="btn" disabled={loading || confirmDisabled} onClick={onConfirm}>
            {loading ? '处理中…' : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}
