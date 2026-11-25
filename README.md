# shttodo

A stupidly simple, self-hosted, agent-first task manager with persistent Telegram reminders.

Designed primarily for AI agents, but equally usable by humans through a CLI.

## Features

- Tasks with a title, group, priority (`low`, `normal`, `high`), due date and notes.
- Groups for projects, companies or personal activities. `master` is the permanent default group.
- Daily habits with history, streaks and backfill of missed marks.
- Telegram reminders at a fixed interval during active hours, until each item is done.
- Done buttons in Telegram for tasks and habits.
- Capture from the phone: send the bot `Review PRs #work !high @tomorrow`, `/habit Gym` or `/list`. Send `/help` for the syntax.

## Agent-first

- `shttodo` prints usage and points agents to `shttodo skill`.
- `shttodo skill` prints the full agent skill, embedded in the binary from [`SKILL.md`](SKILL.md).
- Every command accepts `--json`. Errors have stable codes and exit with code 1.
- Commands that change data return the full object, so no follow-up request is needed.
- No MCP server. The CLI is the integration.

## CLI

```bash
shttodo add "Review PRs" --group work --priority high --due 2026-12-01 --notes "focus on auth"
shttodo list [--group work] [--all] [--json]
shttodo show 12
shttodo edit 12 --title "Review all PRs" --due ""
shttodo done 12
shttodo reopen 12
shttodo delete 12

shttodo group create work
shttodo group list
shttodo group rename work job
shttodo group delete job

shttodo habit add "Gym"
shttodo habit list
shttodo habit done 3 [--date 2026-11-23]
shttodo habit undo 3
shttodo habit show 3 [--since 2026-01-01]
```

## Configuration

CLI:

| Variable | Meaning |
|---|---|
| `SHTTODO_SERVER_URL` | backend URL |
| `SHTTODO_API_TOKEN` | API token |

Server (`shttodo serve`):

| Variable | Default | Meaning |
|---|---|---|
| `SHTTODO_API_TOKEN` | required | API token |
| `TELEGRAM_BOT_TOKEN` | required | bot token from `@BotFather` |
| `TELEGRAM_CHAT_ID` | required | the only chat the bot talks to |
| `REMINDER_INTERVAL_MINUTES` | `60` | must divide 1440 |
| `REMINDER_ACTIVE_HOURS` | `09-21` | inclusive, `22-06` wraps past midnight |
| `TZ` | system | IANA timezone for reminders, due dates and habit days |
| `SHTTODO_DATABASE_PATH` | `shttodo.db` | SQLite file (`/data/shttodo.db` in Docker) |
| `SHTTODO_LISTEN_ADDRESS` | `:8080` | HTTP listen address |

## Deployment

Docker Compose on a VPS, private through Tailscale. See [docs/deploy-coolify-tailscale.md](docs/deploy-coolify-tailscale.md).

## Development

```bash
go test ./...
go build .
```

## Architecture

- One Go binary for the CLI and the backend. Standard library plus `modernc.org/sqlite` (pure Go, no cgo).
- SQLite, REST API with a Bearer token, Telegram Bot API with long polling, built-in scheduler.

## Principles

- Agent-first, human-friendly.
- Self-hosted, single-user, self-documenting.
- Minimal dependencies and minimal token consumption.
- No web dashboard, no embedded AI models, no paid services.

**Get shit done.**
