// The tab strip of open issues (lib/tabs.ts): click to switch, drag to
// reorder, middle-click or ✕ to close, right-click for more.
import { useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { useAIActive } from '../lib/ai-active'
import { closeTabs, type IssueTab, moveTab, useClosedTabs, useIssueTabs } from '../lib/tabs'
import { Icon } from './icons'
import { StatusIcon } from './issue-bits'
import { type Anchor, Menu, type MenuItem, MenuButton, useToast } from './ui'

export function IssueTabs() {
  const tabs = useIssueTabs()
  const closed = useClosedTabs()
  const loc = useLocation()
  const navigate = useNavigate()
  const { toast } = useToast()
  const [drag, setDrag] = useState('')
  const [menu, setMenu] = useState<{ ref: string; anchor: Anchor } | null>(null)
  const m = /^\/issue\/([^/]+)/.exec(loc.pathname)
  const active = m ? decodeURIComponent(m[1]).toUpperCase() : ''
  if (!tabs.length) return null
  const refs = tabs.map(t => t.ref)
  const close = (list: string[]) => {
    closeTabs(list)
    if (active && list.includes(active)) { // closing the open tab: go to its neighbour
      const left = refs.filter(r => !list.includes(r))
      const to = left[Math.min(refs.indexOf(active), left.length - 1)]
      navigate(to ? `/issue/${to}` : '/my-issues')
    }
  }
  const i = refs.indexOf(active)
  const tabMenu = (ref: string): MenuItem[] => {
    const at = refs.indexOf(ref)
    return [
      { label: 'Copy link', icon: 'link', onSelect: () => { navigator.clipboard.writeText(`${location.origin}/issue/${ref}`).then(() => toast('Link copied'), () => toast('Could not copy', 'err')) } },
      'sep',
      { label: 'Close tab', icon: 'x', onSelect: () => close([ref]) },
      { label: 'Close other tabs', disabled: refs.length < 2, onSelect: () => close(refs.filter(r => r !== ref)) },
      { label: 'Close tabs to the right', disabled: at === refs.length - 1, onSelect: () => close(refs.slice(at + 1)) },
    ]
  }
  return (
    <div className="tabbar" role="tablist" aria-label="Open issues">
      <div className="tabbar-scroll">
        {tabs.map(t => (
          <Tab key={t.ref} tab={t} on={t.ref === active} dragging={drag === t.ref}
            onOpen={() => navigate(`/issue/${t.ref}`)} onClose={() => close([t.ref])}
            onMenu={anchor => setMenu({ ref: t.ref, anchor })}
            onDragStart={() => setDrag(t.ref)} onDragEnd={() => setDrag('')} onDragOver={() => { if (drag && drag !== t.ref) moveTab(drag, t.ref) }} dragActive={!!drag} />
        ))}
      </div>
      <MenuButton label="Tab options" className="tab-more" align="right" width={260} items={() => [
        { label: 'Close this tab', icon: 'x', disabled: i < 0, onSelect: () => close([active]) },
        { label: 'Close other tabs', disabled: i < 0 || refs.length < 2, onSelect: () => close(refs.filter(r => r !== active)) },
        { label: 'Close tabs to the right', disabled: i < 0 || i === refs.length - 1, onSelect: () => close(refs.slice(i + 1)) },
        ...(closed.length ? ['sep' as const, { heading: 'Recently closed' }, ...closed.slice(0, 8).map(c => ({
          label: <span className="ellipsis"><b className="tab-ref">{c.ref}</b> {c.title}</span>, icon: 'undo', onSelect: () => navigate(`/issue/${c.ref}`),
        }))] : []),
        'sep',
        { label: 'Close all tabs', danger: true, onSelect: () => close(refs) },
      ]}><Icon name="more" size={16} /></MenuButton>
      {menu && <Menu items={tabMenu(menu.ref)} anchor={menu.anchor} onClose={() => setMenu(null)} width={230} />}
    </div>
  )
}

function Tab({ tab, on, dragging, dragActive, onOpen, onClose, onMenu, onDragStart, onDragEnd, onDragOver }: {
  tab: IssueTab; on: boolean; dragging: boolean; dragActive: boolean; onOpen: () => void; onClose: () => void; onMenu: (a: Anchor) => void
  onDragStart: () => void; onDragEnd: () => void; onDragOver: () => void
}) {
  const ai = useAIActive(tab.ref)
  return (
    <div role="tab" aria-selected={on} tabIndex={0} draggable className={`tab ${on ? 'on' : ''} ${dragging ? 'dragging' : ''}`}
      title={`${tab.ref} · ${tab.title}${ai ? ai.status === 'waiting' ? ' — Pocket AI is waiting for you' : ' — Pocket AI is working' : ''}`}
      onClick={onOpen}
      onKeyDown={e => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onOpen() } }}
      onContextMenu={e => { e.preventDefault(); onMenu({ x: e.clientX, y: e.clientY, w: 0, h: 0 }) }}
      onAuxClick={e => { if (e.button === 1) { e.preventDefault(); onClose() } }}
      onMouseDown={e => { if (e.button === 1) e.preventDefault() }}
      onDragStart={e => { onDragStart(); e.dataTransfer.effectAllowed = 'move' }}
      onDragEnd={onDragEnd}
      onDragOver={e => { if (dragActive) { e.preventDefault(); onDragOver() } }}>
      <span className="tab-ico">{ai ? <span className={`code-pulse ${ai.status}`} role="img" aria-label={ai.status === 'waiting' ? 'Waiting for you' : 'Working'} /> : <StatusIcon type={tab.type} size={13} />}</span>
      <span className="tab-title"><b className="tab-ref">{tab.ref}</b> {tab.title}</span>
      <button type="button" className="tab-x" aria-label="Close tab" title="Close tab (middle-click)" onClick={e => { e.stopPropagation(); onClose() }}><Icon name="x" size={12} /></button>
    </div>
  )
}
