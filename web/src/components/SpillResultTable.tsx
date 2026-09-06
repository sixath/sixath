import { useEffect, useMemo, useState } from 'react'
import { chatApi } from '../api/client'
import type { TimelineNode } from '../pages/timelineReducer'
import { dropIncompleteMarkdownTable, parseSpillItems, pickSpillPath } from '../utils/spillTable'
import { claimedTableSize, truncatedTableHint } from '../utils/truncatedTable'

export function useSpillTable(sessionId: string | undefined, content: string, nodes: TimelineNode[]) {
  const path = useMemo(
    () => (sessionId ? pickSpillPath(sessionId, claimedTableSize(content) ?? 0, nodes) : null),
    [sessionId, content, nodes],
  )
  const hint = useMemo(() => truncatedTableHint(content), [content])
  const [rows, setRows] = useState<{ columns: string[]; rows: string[][] } | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!sessionId || !path || !hint) {
      setRows(null)
      setError('')
      setLoading(false)
      return
    }
    let cancelled = false
    setLoading(true)
    setError('')
    chatApi
      .listResultFile(sessionId, path)
      .then((res) => {
        if (cancelled) return
        const table = parseSpillItems(res.items)
        setRows(table.rows.length ? table : null)
      })
      .catch((e) => {
        if (cancelled) return
        setRows(null)
        setError((e as Error).message)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [sessionId, path, hint])

  return {
    hint,
    path,
    table: rows,
    loading,
    error,
    displayContent: rows && rows.rows.length ? dropIncompleteMarkdownTable(content) : content,
  }
}

export function SpillResultTable({
  columns,
  rows,
}: {
  columns: string[]
  rows: string[][]
}) {
  return (
    <div className="spill-table-wrap">
      <div className="spill-table-caption">完整结果 {rows.length} 行（来自工具落盘文件）</div>
      <table className="spill-table">
        <thead>
          <tr>
            {columns.map((c) => (
              <th key={c}>{c}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, i) => (
            <tr key={i}>
              {row.map((cell, j) => (
                <td key={j}>{cell}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
