import { Modal } from './ui'

const GROUPS: { title: string; keys: [string[], string][] }[] = [
  { title: 'Anywhere', keys: [[['⌘', 'K'], 'Search & commands'], [['C'], 'New issue'], [['/'], 'Search page'], [[','], 'Settings'], [['['], 'Show or hide the sidebar'], [['?'], 'This sheet'], [['esc'], 'Close a dialog or menu']] },
  { title: 'Go to', keys: [[['G', 'I'], 'Inbox'], [['G', 'M'], 'My issues'], [['G', 'P'], 'Projects'], [['G', 'R'], 'Roadmap'], [['G', 'V'], 'Views'], [['G', 'C'], 'Calendar'], [['G', 'T'], 'Time'], [['G', 'N'], 'Insights']] },
  { title: 'In a list or board', keys: [[['J', 'K'], 'Move down / up'], [['↵'], 'Open the issue'], [['X'], 'Select'], [['⇧', 'click'], 'Select a range'], [['⌘', 'A'], 'Select all'], [['B'], 'List / board']] },
  { title: 'The focused or selected issues', keys: [[['S'], 'Status'], [['A'], 'Assignee'], [['I'], 'Assign to me'], [['P'], 'Priority'], [['L'], 'Labels'], [['D'], 'Due date'], [['E'], 'Estimate'], [['⌫'], 'Move to the trash']] },
  { title: 'On an issue', keys: [[['⌘', '↵'], 'Send the comment'], [['M'], 'Comment'], [['⌘', '.'], 'Copy the ID']] },
]

export function ShortcutsSheet({ onClose }: { onClose: () => void }) {
  return (
    <Modal title="Keyboard shortcuts" onClose={onClose} size="lg">
      <div className="shortcuts">
        {GROUPS.map(g => (
          <section key={g.title}>
            <h4 className="sc-h">{g.title}</h4>
            {g.keys.map(([keys, label]) => (
              <div key={label + keys.join('')} className="sc-row"><span>{label}</span><span className="sc-keys">{keys.map(k => <kbd key={k}>{k}</kbd>)}</span></div>
            ))}
          </section>
        ))}
      </div>
      <p className="muted small mt">In descriptions and comments: <code>@name</code> mentions someone, <code>#</code> links an issue, identifiers like <code>WEB-12</code> become links, images can be pasted.</p>
    </Modal>
  )
}
