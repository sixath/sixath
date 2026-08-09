import { useCallback, useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { agentApi, chatApi, type Agent, type AgentInsights } from '../api/client'

export default function AgentInsightsPage() {
  const { id } = useParams()
  const [agent, setAgent] = useState<Agent | null>(null)
  const [report, setReport] = useState<AgentInsights | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    if (!id) return
    setLoading(true)
    setError('')
    try {
      const [a, insights] = await Promise.all([
        agentApi.get(id),
        chatApi.getInsights(id),
      ])
      setAgent(a)
      setReport(insights)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [id])

  useEffect(() => {
    void load()
  }, [load])

  if (!id) return <p>Missing agent id</p>
  if (loading) return <div className="page-header"><p>Loading insights…</p></div>
  if (error) {
    return (
      <div>
        <div className="page-header">
          <h1>Insights</h1>
          <Link to={`/agents/${id}`} className="btn btn-secondary btn-sm">Back</Link>
        </div>
        <p className="error">{error}</p>
      </div>
    )
  }

  const pct = report ? (report.error_rate * 100).toFixed(1) : '0.0'

  return (
    <div>
      <div className="page-header">
        <h1>Insights{agent ? ` · ${agent.name}` : ''}</h1>
        <div className="actions">
          <button type="button" className="btn btn-secondary btn-sm" onClick={() => void load()}>Refresh</button>
          <Link to={`/agents/${id}/chat`} className="btn btn-sm">Chat</Link>
          <Link to={`/agents/${id}`} className="btn btn-secondary btn-sm">Back</Link>
        </div>
      </div>

      {!report ? (
        <p>No data</p>
      ) : (
        <>
          <section className="section">
            <h2 className="section-title">Summary</h2>
            <div className="section-card">
              <p><strong>Window:</strong> {String(report.from)} → {String(report.to)}</p>
              <p><strong>Turns:</strong> {report.turns}</p>
              <p><strong>Tool calls:</strong> {report.tool_calls}</p>
              <p><strong>Errors:</strong> {report.error_calls} ({pct}%)</p>
              <p><strong>Blocked:</strong> {report.blocked_calls}</p>
              {report.truncated ? <p className="muted">Scan truncated at row cap.</p> : null}
            </div>
          </section>

          <section className="section">
            <h2 className="section-title">Top tools</h2>
            <div className="section-card">
              <table className="table">
                <thead>
                  <tr>
                    <th>Tool</th>
                    <th>Calls</th>
                    <th>Errors</th>
                  </tr>
                </thead>
                <tbody>
                  {(report.top_tools || []).map((t) => (
                    <tr key={t.name}>
                      <td><code>{t.name}</code></td>
                      <td>{t.calls}</td>
                      <td>{t.errors}</td>
                    </tr>
                  ))}
                  {(report.top_tools || []).length === 0 ? (
                    <tr><td colSpan={3}>No tool traffic in window</td></tr>
                  ) : null}
                </tbody>
              </table>
            </div>
          </section>

          <section className="section">
            <h2 className="section-title">Top sessions</h2>
            <div className="section-card">
              <table className="table">
                <thead>
                  <tr>
                    <th>Session</th>
                    <th>Turns</th>
                    <th>Errors</th>
                  </tr>
                </thead>
                <tbody>
                  {(report.top_sessions || []).map((s) => (
                    <tr key={s.session_id}>
                      <td>
                        <Link to={`/agents/${id}/chat/${s.session_id}`}>
                          <code>{s.session_id.slice(0, 8)}…</code>
                        </Link>
                      </td>
                      <td>{s.turns}</td>
                      <td>{s.errors}</td>
                    </tr>
                  ))}
                  {(report.top_sessions || []).length === 0 ? (
                    <tr><td colSpan={3}>No sessions</td></tr>
                  ) : null}
                </tbody>
              </table>
            </div>
          </section>
        </>
      )}
    </div>
  )
}
