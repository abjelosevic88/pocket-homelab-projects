import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter, Navigate, Route, Routes, useLocation } from 'react-router-dom'
import './styles/base.css'
import './styles/app.css'
import './styles/settings.css'
import './styles/projects.css'
import { AppProvider, useApp } from './lib/app-context'
import { Loading, ToastProvider } from './components/ui'
import Layout from './components/Layout'
import { ErrorBoundary } from './components/ErrorBoundary'
import Login from './pages/Login'
import TeamIssues from './pages/TeamIssues'
import TeamAI from './pages/TeamAI'
import TeamNotes from './pages/TeamNotes'
import ServerPage from './pages/ServerPage'
import IssuePage from './pages/IssuePage'
import MyIssues from './pages/MyIssues'
import ViewsPage from './pages/ViewsPage'
import ViewPage from './pages/ViewPage'
import Trash from './pages/Trash'
import Projects from './pages/Projects'
import ProjectPage from './pages/ProjectPage'
import Roadmap from './pages/Roadmap'
import DocPage from './pages/DocPage'
import SharedNotePage from './pages/SharedNotePage'
import Inbox from './pages/Inbox'
import Cycles from './pages/Cycles'
import CyclePage from './pages/CyclePage'
import Insights from './pages/Insights'
import CalendarPage from './pages/CalendarPage'
import TimePage from './pages/TimePage'
import Search from './pages/Search'
import Settings from './pages/Settings'
import TeamSettings from './pages/TeamSettings'
import NewTeam from './pages/NewTeam'
import Teams from './pages/Teams'

function Gate({ children }: { children: React.ReactNode }) {
  const { status, user, boot } = useApp()
  const loc = useLocation()
  if (!status) return <div className="boot"><img src="/favicon.svg" alt="" /><Loading /></div>
  if (status.needs_setup) return <Navigate to="/setup" replace />
  if (!user) return <Navigate to="/login" replace state={{ from: loc.pathname + loc.search }} />
  if (!boot) return <div className="boot"><img src="/favicon.svg" alt="" /><Loading /></div>
  return <>{children}</>
}

function Home() {
  const { boot, settings } = useApp()
  if (!boot?.teams.length) return <Navigate to="/teams/new?welcome=1" replace />
  const to = { inbox: '/inbox', projects: '/projects', team: `/team/${boot.teams[0].key}/issues` }[settings?.home_view || ''] || '/my-issues'
  return <Navigate to={to} replace />
}

function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login mode="login" />} />
      <Route path="/setup" element={<Login mode="setup" />} />
      <Route path="/settings/:tab?" element={<Gate><ErrorBoundary where="settings"><Settings /></ErrorBoundary></Gate>} />
      <Route path="/embed/issue/:ref/shared/:id" element={<Gate><ErrorBoundary where="shared note"><SharedNotePage embed /></ErrorBoundary></Gate>} />
      <Route element={<Gate><Layout /></Gate>}>
        <Route path="/" element={<Home />} />
        <Route path="/inbox" element={<Inbox />} />
        <Route path="/my-issues/:tab?" element={<MyIssues />} />
        <Route path="/issue/:ref" element={<IssuePage />} />
        <Route path="/issue/:ref/shared/:id" element={<SharedNotePage />} />
        <Route path="/team/:key" element={<Navigate to="issues" replace />} />
        <Route path="/team/:key/:tab" element={<TeamIssues />} />
        <Route path="/team/:key/ai" element={<TeamAI />} />
        <Route path="/team/:key/notes" element={<TeamNotes />} />
        <Route path="/team/:key/server" element={<ServerPage />} />
        <Route path="/team/:key/projects" element={<Projects />} />
        <Route path="/team/:key/cycles" element={<Cycles />} />
        <Route path="/team/:key/cycles/:id" element={<CyclePage />} />
        <Route path="/team/:key/settings/:tab?" element={<TeamSettings />} />
        <Route path="/teams" element={<Teams />} />
        <Route path="/teams/new" element={<NewTeam />} />
        <Route path="/projects" element={<Projects />} />
        <Route path="/project/:id/:tab?" element={<ProjectPage />} />
        <Route path="/doc/:id" element={<DocPage />} />
        <Route path="/roadmap" element={<Roadmap />} />
        <Route path="/views" element={<ViewsPage />} />
        <Route path="/view/:id" element={<ViewPage />} />
        <Route path="/insights" element={<Insights />} />
        <Route path="/calendar" element={<CalendarPage />} />
        <Route path="/time" element={<TimePage />} />
        <Route path="/search" element={<Search />} />
        <Route path="/trash" element={<Trash />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <BrowserRouter>
      <ErrorBoundary where="app"><ToastProvider><AppProvider><App /></AppProvider></ToastProvider></ErrorBoundary>
    </BrowserRouter>
  </React.StrictMode>,
)
