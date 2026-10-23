// Shared bits of the team settings tabs.
import { useCallback } from 'react'
import { api, V1 } from '../../lib/api'
import { useApp } from '../../lib/app-context'
import type { Team, TeamRole } from '../../lib/types'
import { Icon } from '../../components/icons'
import { errMsg, useToast } from '../../components/ui'

export interface TabProps { team: Team; admin: boolean; canWrite: boolean }

export const ROLE_LABEL: Record<TeamRole, string> = { admin: 'Admin', member: 'Member', guest: 'Guest' }
export const ROLE_HELP: Record<TeamRole, string> = {
  admin: 'Everything a member can, plus team settings, workflow, members, templates, webhooks and deleting the team.',
  member: 'Creates and changes issues, projects, cycles and shared views; comments.',
  guest: 'Reads the team and comments; can’t change issues or projects. Their requests land in Triage.',
}

/** PATCH the team, reload the app's data and toast errors. Resolves to the saved team or null. */
export function usePatchTeam(team: Team) {
  const { reload } = useApp()
  const { toast } = useToast()
  return useCallback(async (body: Record<string, unknown>, ok?: string): Promise<Team | null> => {
    try {
      const t = await api.patch<Team>(`${V1}/teams/${team.key}`, body)
      await reload()
      if (ok) toast(ok)
      return t
    } catch (e) { toast(errMsg(e), 'err'); return null }
  }, [team.key, reload, toast])
}

export function ReadOnly({ what = 'these settings' }: { what?: string }) {
  return <div className="callout"><Icon name="lock" size={15} /><span>Only team admins can change {what}. You can look around.</span></div>
}

/** A drag handle (six dots). */
export function Grip() {
  return <svg className="ui-grip" width="10" height="16" viewBox="0 0 10 16" aria-hidden>{[3, 8, 13].flatMap(y => [2.5, 7.5].map(x => <circle key={`${x}-${y}`} cx={x} cy={y} r="1.3" fill="currentColor" />))}</svg>
}
