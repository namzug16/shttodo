# shttodo

shttodo is a simple task manager. You host it on your own server. It sends Telegram reminders until you complete each task.

AI agents are the primary users. People can also use it through the CLI.

## Features

- Each task has a title, a group, a priority (`low`, `normal`, `high`), a due date and notes.
- The priority increases near the due date. It is `high` 7 days before the due date and after the due date. It is minimum `normal` 30 days before the due date.
- Groups keep tasks for projects, companies or personal work apart. `master` is the default group. You cannot delete it.
- Daily habits have a history and streaks. You can mark the days that you missed.
- During the active hours, Telegram sends reminders at a fixed interval until you complete each item.
- Each Telegram reminder has a "Done" button for tasks and habits.
- An agent CLI, for example Codex, can write the reminder text. It can also add tasks from your own words: "deliver the report in three days". Refer to [docs/reminder-agent.md](docs/reminder-agent.md).
- You can add items from your phone. Send the bot `Review PRs #work !high @tomorrow`, `/habit Gym` or `/list`. Send `/help` to see the syntax.

## Agent-first

- `shttodo` shows the usage and tells agents to use `shttodo skill`.
- `shttodo skill` shows the full agent skill. The binary contains it from [`SKILL.md`](SKILL.md).
- All commands accept `--json`. Errors have fixed codes. When an error occurs, the exit code is 1.
- Commands that change data return the full object. Thus, the agent does not need a second request.
- There is no MCP server. The CLI is the integration.

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

## Get the tokens

The server needs three values: `SHTTODO_API_TOKEN`, `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID`.

### Make the API token

You make this token yourself. The server does not make it, and the CLI does not make it. It is a shared password between the server and the CLI.

1. Run this command on any computer:

   ```bash
   openssl rand -hex 32
   ```

2. Copy the output. This is `SHTTODO_API_TOKEN`.
3. On the server, set `SHTTODO_API_TOKEN` to this value.
4. On the computer with the CLI, set `SHTTODO_API_TOKEN` to the same value.

The CLI sends the token with each request. If the two values are different, the server refuses the request.

### Make the Telegram bot

1. In Telegram, open a chat with [`@BotFather`](https://t.me/BotFather).
2. Send `/newbot`.
3. Type a display name for the bot, for example `My Tasks`.
4. Type a username for the bot. The username must end with `bot`, for example `my_tasks_bot`.
5. Copy the token that `@BotFather` sends. This is `TELEGRAM_BOT_TOKEN`. It looks like `123456789:AAH...`.

### Get the chat ID

1. Open a chat with your new bot. Push **Start**, or send a message, for example `hello`.
2. Make sure that shttodo is not in operation. When shttodo is in operation, it reads the messages first, and the next step shows no messages.
3. Open this URL in a browser. Replace `<bot-token>` with your bot token:

   ```
   https://api.telegram.org/bot<bot-token>/getUpdates
   ```

4. Find `"chat":{"id":...}` in the response. Copy the number. This is `TELEGRAM_CHAT_ID`.

If the `result` list is empty, send a new message to the bot. Then open the URL again.

To use the bot in a group, add the bot to the group and send a message in the group. A group chat ID starts with `-`.

### Keep the tokens safe

- Keep the tokens in a password manager.
- Do not commit the tokens to Git.
- If a person gets your bot token, send `/revoke` to `@BotFather`. Then use the new token.

## Configuration

Copy [`.env.example`](.env.example) to `.env` and set the values. Docker Compose reads `.env` automatically.

CLI:

| Variable | Meaning |
|---|---|
| `SHTTODO_SERVER_URL` | the URL of the server |
| `SHTTODO_API_TOKEN` | the API token |

Server (`shttodo serve`):

| Variable | Default | Meaning |
|---|---|---|
| `SHTTODO_API_TOKEN` | required | the API token |
| `TELEGRAM_BOT_TOKEN` | required | the bot token from `@BotFather` |
| `TELEGRAM_CHAT_ID` | required | the only chat that the bot uses |
| `REMINDER_INTERVAL_MINUTES` | `60` | 1440 divided by this value must give a whole number |
| `REMINDER_ACTIVE_HOURS` | `09-21` | includes the two hours. `22-06` continues after midnight |
| `REMINDER_AGENT_COMMAND` | empty | a shell command that writes the reminder text and reads the messages to the bot. Refer to [docs/reminder-agent.md](docs/reminder-agent.md) |
| `REMINDER_AGENT_PROMPT` | built-in | the instructions for the agent |
| `TZ` | system | the IANA timezone for reminders, due dates and habit days |
| `SHTTODO_DATABASE_PATH` | `shttodo.db` | the SQLite file (`/data/shttodo.db` in Docker) |
| `SHTTODO_LISTEN_ADDRESS` | `:8080` | the HTTP listen address |

## Deployment

Use Docker Compose on a VPS. Tailscale keeps the server private. Refer to [docs/deploy-coolify-tailscale.md](docs/deploy-coolify-tailscale.md).

## Development

```bash
go test ./...
go build .
```

## Architecture

- One Go binary contains the CLI and the server. It uses the standard library and `modernc.org/sqlite` (pure Go, no cgo).
- The server uses SQLite, a REST API with a Bearer token, the Telegram Bot API with long polling and a built-in scheduler.

## Principles

- Agents are the primary users. People can also use it easily.
- You host it yourself. It has one user. It shows its own documentation.
- It has few dependencies and uses few tokens.
- It has no web dashboard, no AI models in the binary and no paid services. An agent CLI is optional.

**Get shit done.**
