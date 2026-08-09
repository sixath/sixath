import { useState, useEffect } from 'react'
import { BrowserRouter, Routes, Route, NavLink, useLocation, useNavigate } from 'react-router-dom'
import ToolList from './pages/ToolList'
import ToolForm from './pages/ToolForm'
import McpServerList from './pages/McpServerList'
import McpServerForm from './pages/McpServerForm'
import AgentList from './pages/AgentList'
import AgentForm from './pages/AgentForm'
import AgentDetail from './pages/AgentDetail'
import AgentInsightsPage from './pages/AgentInsightsPage'
import ChatPage from './pages/ChatPage'
import ChatHome from './pages/ChatHome'
import ChannelList from './pages/ChannelList'
import ChannelForm from './pages/ChannelForm'
import CronTaskList from './pages/CronTaskList'
import CronTaskForm from './pages/CronTaskForm'
import CronTaskDetail from './pages/CronTaskDetail'
import SessionHistoryPage from './pages/SessionHistoryPage'
import SettingsPage from './pages/SettingsPage'
import OrgListPage from './pages/OrgListPage'
import OrgDetailPage from './pages/OrgDetailPage'
import LoginPage from './pages/LoginPage'
import RegisterPage from './pages/RegisterPage'
import VerifyEmailPage from './pages/VerifyEmailPage'
import RequireAuth from './components/RequireAuth'
import { hasApiToken, isSessionEmailUnverified, logout } from './api/auth'
import './App.css'

function Breadcrumb() {
  const loc = useLocation()
  const path = loc.pathname
  const segments = path.split('/').filter(Boolean)

  let current = '对话'
  let icon = '💬'
  if (segments.length === 0) {
    current = '对话'
    icon = '💬'
  } else if (segments[0] === 'tools') {
    if (segments[1] === 'new') { current = '新建工具'; icon = '➕' }
    else if (segments[2] === 'edit') { current = '编辑工具'; icon = '✏️' }
    else if (segments[1]) { current = '工具详情'; icon = '🔧' }
    else { current = '工具管理'; icon = '🔧' }
  } else if (segments[0] === 'mcp-servers') {
    if (segments[1] === 'new') { current = '新建 MCP 服务'; icon = '➕' }
    else if (segments[2] === 'edit') { current = '编辑 MCP 服务'; icon = '✏️' }
    else if (segments[1]) { current = 'MCP 服务详情'; icon = '🔌' }
    else { current = 'MCP 服务'; icon = '🔌' }
  } else if (segments[0] === 'agents') {
    if (segments[1] === 'new') { current = '新建 Agent'; icon = '➕' }
    else if (segments[2] === 'edit') { current = '编辑 Agent'; icon = '✏️' }
    else if (segments[2] === 'chat') { current = '对话'; icon = '💬' }
    else if (segments[1]) { current = 'Agent 详情'; icon = '🤖' }
    else { current = 'Agent 管理'; icon = '🤖' }
  } else if (segments[0] === 'channels') {
    if (segments[1] === 'new') { current = '新建渠道'; icon = '➕' }
    else if (segments[2] === 'edit') { current = '编辑渠道'; icon = '✏️' }
    else if (segments[1]) { current = '渠道详情'; icon = '📡' }
    else { current = '渠道管理'; icon = '📡' }
  } else if (segments[0] === 'cron') {
    if (segments[1] === 'new') { current = '新建定时任务'; icon = '➕' }
    else if (segments[2] === 'edit') { current = '编辑定时任务'; icon = '✏️' }
    else if (segments[1]) { current = '任务详情'; icon = '⏰' }
    else { current = '定时任务'; icon = '⏰' }
  } else if (segments[0] === 'sessions') {
    current = '会话历史'
    icon = '📋'
  } else if (segments[0] === 'settings') {
    current = '设置'
    icon = '⚙️'
  } else if (segments[0] === 'orgs') {
    if (segments[1]) { current = '组织详情'; icon = '🏢' }
    else { current = '组织'; icon = '🏢' }
  }

  return (
    <div className="topbar__breadcrumb">
      <span className="topbar__breadcrumb-icon">{icon}</span>
      <span className="topbar__breadcrumb-current">{current}</span>
    </div>
  )
}

const SIDEBAR_KEY = 'sixath-sidebar-open'

function AppShell() {
  const loc = useLocation()
  const navigate = useNavigate()
  const tokenConfigured = hasApiToken()
  // re-check when navigating (e.g. after saving Settings)
  void loc.pathname

  const [navOpen, setNavOpen] = useState(() => {
    try {
      const v = localStorage.getItem(SIDEBAR_KEY)
      return v === null ? true : v === '1'
    } catch {
      return true
    }
  })

  useEffect(() => {
    try {
      localStorage.setItem(SIDEBAR_KEY, navOpen ? '1' : '0')
    } catch {}
  }, [navOpen])

  return (
    <div className={`shell ${navOpen ? '' : 'shell-nav-collapsed'}`}>
      <aside className="shell-nav">
        <div className="sidebar-brand">
          <div className="sidebar-brand__logo" />
          <span className="sidebar-brand__title">SIXATH</span>
          <button
            type="button"
            className="sidebar-toggle"
            onClick={() => setNavOpen(false)}
            title="收起导航栏"
            aria-label="收起导航栏"
          >
            ◀
          </button>
        </div>
        <nav className="sidebar-nav">
          <div className="nav-section">
            <NavLink to="/" end className={({ isActive }) => `nav-item ${isActive ? 'active' : ''}`}>
              <span className="nav-item__icon">💬</span>
              对话
            </NavLink>
            <NavLink to="/sessions" className={({ isActive }) => `nav-item ${isActive ? 'active' : ''}`}>
              <span className="nav-item__icon">📋</span>
              会话历史
            </NavLink>
            <NavLink to="/tools" className={({ isActive }) => `nav-item ${isActive ? 'active' : ''}`}>
              <span className="nav-item__icon">🔧</span>
              工具管理
            </NavLink>
            <NavLink to="/mcp-servers" className={({ isActive }) => `nav-item ${isActive ? 'active' : ''}`}>
              <span className="nav-item__icon">🔌</span>
              MCP 服务
            </NavLink>
            <NavLink to="/agents" className={({ isActive }) => `nav-item ${isActive ? 'active' : ''}`}>
              <span className="nav-item__icon">🤖</span>
              Agent 管理
            </NavLink>
            <NavLink to="/channels" className={({ isActive }) => `nav-item ${isActive ? 'active' : ''}`}>
              <span className="nav-item__icon">📡</span>
              渠道管理
            </NavLink>
            <NavLink to="/cron" className={({ isActive }) => `nav-item ${isActive ? 'active' : ''}`}>
              <span className="nav-item__icon">⏰</span>
              定时任务
            </NavLink>
            <NavLink to="/orgs" className={({ isActive }) => `nav-item ${isActive ? 'active' : ''}`}>
              <span className="nav-item__icon">🏢</span>
              组织
            </NavLink>
            <NavLink to="/settings" className={({ isActive }) => `nav-item ${isActive ? 'active' : ''}`}>
              <span className="nav-item__icon">⚙️</span>
              设置{tokenConfigured ? ' ·已登录' : ' ·!'}
            </NavLink>
            <button
              type="button"
              className="nav-item"
              onClick={() => {
                logout()
                navigate('/login', { replace: true })
              }}
            >
              <span className="nav-item__icon">⎋</span>
              退出
            </button>
          </div>
        </nav>
      </aside>
      <header className="topbar">
        {!navOpen && (
          <button
            type="button"
            className="topbar-nav-toggle"
            onClick={() => setNavOpen(true)}
            title="展开导航栏"
            aria-label="展开导航栏"
          >
            ▶
          </button>
        )}
        <Breadcrumb />
      </header>
      <main className="content">
        <div className="content-inner">
          {isSessionEmailUnverified() && (
            <p className="email-unverified-banner" role="status">
              邮箱尚未验证。请查收验证邮件，或稍后在设置中确认账号状态。
            </p>
          )}
          <Routes>
            <Route path="/" element={<ChatHome />} />
            <Route path="/sessions" element={<SessionHistoryPage />} />
            <Route path="/tools" element={<ToolList />} />
            <Route path="/tools/new" element={<ToolForm />} />
            <Route path="/tools/:id/edit" element={<ToolForm />} />
            <Route path="/mcp-servers" element={<McpServerList />} />
            <Route path="/mcp-servers/new" element={<McpServerForm />} />
            <Route path="/mcp-servers/:id/edit" element={<McpServerForm />} />
            <Route path="/agents" element={<AgentList />} />
            <Route path="/agents/new" element={<AgentForm />} />
            <Route path="/agents/:id/edit" element={<AgentForm />} />
            <Route path="/agents/:id" element={<AgentDetail />} />
            <Route path="/agents/:id/insights" element={<AgentInsightsPage />} />
            <Route path="/agents/:id/chat" element={<ChatPage />} />
            <Route path="/agents/:id/chat/:sessionId" element={<ChatPage />} />
            <Route path="/channels" element={<ChannelList />} />
            <Route path="/channels/new" element={<ChannelForm />} />
            <Route path="/channels/:id/edit" element={<ChannelForm />} />
            <Route path="/cron" element={<CronTaskList />} />
            <Route path="/cron/new" element={<CronTaskForm />} />
            <Route path="/cron/:id" element={<CronTaskDetail />} />
            <Route path="/cron/:id/edit" element={<CronTaskForm />} />
            <Route path="/orgs" element={<OrgListPage />} />
            <Route path="/orgs/:id" element={<OrgDetailPage />} />
            <Route path="/settings" element={<SettingsPage />} />
          </Routes>
        </div>
      </main>
    </div>
  )
}

function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/register" element={<RegisterPage />} />
        <Route path="/verify-email" element={<VerifyEmailPage />} />
        <Route
          path="/*"
          element={
            <RequireAuth>
              <AppShell />
            </RequireAuth>
          }
        />
      </Routes>
    </BrowserRouter>
  )
}

export default App
