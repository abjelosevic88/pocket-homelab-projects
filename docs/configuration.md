# Configuration

Everything is an environment variable. Nothing is required: `docker run` with one volume works.

| Variable | Default | What it does |
|---|---|---|
| `PORT`, `HOST` | `8080`, `0.0.0.0` | Where to listen |
| `BASE_URL` | | The public address (`https://projects.example.com`). Used in links (notifications, Pocket Calendar events, Pocket Tasks to-dos, events for Pocket Core, the calendar feed, the git webhook address) and for OIDC |
| `DATA_DIR` | `./data` | Database, attachments (`files/`), backups |
| `DB_PATH` | `$DATA_DIR/pocket-projects.db` | |
| `TZ` | `UTC` | The box's time zone: when cycles start and end, when repeating issues are made. Each person can choose their own for due-date reminders |
| `LOG_LEVEL`, `LOG_FORMAT` | `info`, `text` | `debug` logs every request; `json` for log shippers |
| `TRUST_PROXY` | `true` | Read the client address from `X-Forwarded-For` |
| `METRICS_ENABLED` | `true` | `/metrics` for Prometheus |
| `AUTH_MODE` | `local` | `local` (accounts and passwords), `proxy` (trust `AUTH_PROXY_HEADER`), `none` (everyone is the admin; demos only) |
| `AUTH_PROXY_HEADER`, `AUTH_PROXY_EMAIL_HEADER` | `Remote-User`, `Remote-Email` | For `AUTH_MODE=proxy` |
| `ADMIN_EMAIL`, `ADMIN_PASSWORD`, `ADMIN_NAME` | | Create the admin on first start instead of the setup screen (only on an empty database) |
| `SESSION_TTL` | `720h` | |
| `SECURE_COOKIES` | `false` | Set behind HTTPS |
| `OIDC_ISSUER`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET`, `OIDC_NAME`, `OIDC_AUTO_CREATE` | | Sign in with any OpenID Connect provider. Not needed with Pocket Core |
| `CORE_URL`, `CORE_REGISTRATION_KEY`, `CORE_APP_URL` | | Pocket Core: registration, "Sign in with Pocket Core", the household for team invitations, app links to Pocket Calendar / Contacts / Tasks / Notes / AI, events, the MCP tools Pocket AI uses |
| `NOTIFY_URL`, `NOTIFY_KIND` | | The owner's default push route: an Apprise route (`http://apprise:8000/notify/projects`), an ntfy topic URL or a webhook; `apprise` / `ntfy` / `webhook`. Everyone else sets their own in Settings → Notifications |
| `DEMO_DATA` | `false` | Seed two teams, three projects, cycles and issues into an empty box |

## Commands

```
pocket-projects                       start
pocket-projects backup [-o FILE]      consistent SQLite copy (VACUUM INTO)
pocket-projects reset-password [-email USER] [-password NEW]
pocket-projects version
```

## Files in the volume

- `pocket-projects.db` (+ `-wal`, `-shm`): everything except attachments. Back it up with `pocket-projects backup`, never by copying a live file.
- `files/<issue id>/<attachment id>`: uploaded attachments and pasted images.
- `core.token`, `core.token.oidc.json`: the app's Pocket Core credentials.
- `backups/`: snapshots made by the backup command.

## Background work

Once a minute the server makes the issues of repeating templates that are due and sends each person's due-date
reminders at their digest time; every ten minutes it plans cycles ahead, finishes ended cycles (rolling unfinished
issues over) and records the burn-up and cumulative-flow snapshots; once an hour it reminds project leads who owe an
update, archives finished issues past their team's limit, empties the 30-day trash and prunes old notifications.
Everything is idempotent: a restart never makes an issue or a reminder twice.
