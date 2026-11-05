// The key that opens and closes the app's chat (Settings → Preferences):
// "mod+i" style, mod = ⌘ on a Mac, Ctrl elsewhere; "off" = none. Recorded the
// way Pocket AI records its own shortcuts, so the conversation can hand it back.
import { useEffect, useState } from 'react'
import { api, HOME_V1 } from './api'

export const DEFAULT_CHAT_KEY = 'mod+i'
export const isMac = /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent)

/** The key itself, the same on every layout for letters and digits (Option+I types "ˆ"). */
function keyName(e: KeyboardEvent): string {
  const letter = /^Key([A-Z])$/.exec(e.code)
  if (letter) return letter[1].toLowerCase()
  const digit = /^Digit(\d)$/.exec(e.code)
  if (digit) return digit[1]
  const code: Record<string, string> = { BracketLeft: '[', BracketRight: ']', Period: '.', Comma: ',', Slash: '/', Semicolon: ';', Quote: "'", Backquote: '`', Minus: '-', Equal: '=', Backslash: '\\' }
  return code[e.code] || e.key.toLowerCase()
}

/** "mod+shift+l" for a key press, '' for a modifier on its own. */
export function comboOf(e: KeyboardEvent): string {
  if (['Meta', 'Control', 'Shift', 'Alt'].includes(e.key)) return ''
  const parts: string[] = []
  if (e.metaKey || e.ctrlKey) parts.push('mod')
  if (e.altKey) parts.push('alt')
  if (e.shiftKey) parts.push('shift')
  parts.push(keyName(e))
  return parts.join('+')
}

const symbols: Record<string, string> = { mod: isMac ? '⌘' : 'Ctrl', alt: isMac ? '⌥' : 'Alt', shift: '⇧' }
/** "mod+shift+l" → ["⌘", "⇧", "L"]; none for off */
export const keyCaps = (keys: string) => !keys || keys === 'off' ? [] : keys.split('+').map(k => symbols[k] || k.toUpperCase())
/** "⌘⇧L", '' for off */
export const keyLabel = (keys: string) => keyCaps(keys).join(isMac ? '' : '+')

/** Why a combination can't be the chat key ('' = it can). */
export function chatKeyProblem(keys: string): string {
  const mods = keys.split('+').slice(0, -1)
  if (!mods.includes('mod') && !mods.includes('alt')) return `Use ${symbols.mod} or ${symbols.alt} with the key, so typing never opens the chat.`
  const taken: Record<string, string> = {
    'mod+k': 'Search & commands', 'mod+a': 'Select all', 'mod+.': 'Copy the ID', 'mod+c': 'Copy', 'mod+v': 'Paste', 'mod+x': 'Cut', 'mod+z': 'Undo', 'mod+shift+z': 'Redo',
    'mod+e': 'Pocket AI: Recent conversations', 'mod+j': 'Pocket AI: Run a recipe', 'mod+n': 'the browser: New window', 'mod+t': 'the browser: New tab', 'mod+w': 'the browser: Close tab',
    'mod+shift+t': 'Reopen closed tab', 'mod+l': 'the browser: Address bar', 'mod+r': 'the browser: Reload', 'mod+q': 'Quit', 'mod+f': 'Find on the page', 'mod+p': 'Print', 'mod+s': 'Save',
    'mod+b': 'Bold in text editors', 'mod+u': 'Underline in text editors', 'mod+shift+[': 'Pocket AI: Previous tab', 'mod+shift+]': 'Pocket AI: Next tab',
  }
  return taken[keys] ? `${keyLabel(keys)} is ${taken[keys]}.` : ''
}

// Kept on this instance, in every workspace (a connected one's settings are
// its own server's): read once, saved here, every listener told at once.
let current: Promise<string> | null = null
const EVENT = 'pp:chat-key'

function loadChatKey(): Promise<string> {
  current ??= api.get<{ settings?: { chat_key?: string } }>(`${HOME_V1}/settings`).then(r => r.settings?.chat_key || DEFAULT_CHAT_KEY).catch(() => { current = null; return DEFAULT_CHAT_KEY })
  return current
}

export async function saveChatKey(keys: string): Promise<void> {
  await api.put(`${HOME_V1}/settings`, { chat_key: keys })
  current = Promise.resolve(keys)
  window.dispatchEvent(new CustomEvent(EVENT, { detail: keys }))
}

/** The person's chat key ("off" = none). */
export function useChatKey(): string {
  const [keys, setKeys] = useState(DEFAULT_CHAT_KEY)
  useEffect(() => {
    let live = true
    loadChatKey().then(k => live && setKeys(k))
    const h = (e: Event) => setKeys(String((e as CustomEvent<string>).detail))
    window.addEventListener(EVENT, h)
    return () => { live = false; window.removeEventListener(EVENT, h) }
  }, [])
  return keys
}
