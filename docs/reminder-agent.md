# Use an agent for reminders and new tasks

By default, each reminder is a plain list of the pending items. You can tell the server to run an agent CLI. The agent then writes the reminder text. It also changes the messages that you send to the bot into tasks. Refer to [Add tasks in your own words](#add-tasks-in-your-own-words).

The buttons do not change. Each pending item keeps its "Done" button.

## How it works

1. At each reminder time, the server makes the plain list.
2. The server runs `REMINDER_AGENT_COMMAND` with `sh -c`.
3. The server writes this text to the stdin of the command:

   ```
   <REMINDER_AGENT_PROMPT>

   Now: Saturday 2026-11-28 15:00

   Pending items:
   Today
     ⬜ Gym

   work
     #12 ⚠️ Review PRs (high) due 2026-11-27
   ```

4. The server sends the stdout of the command as the message text.

Any CLI that reads a prompt on stdin and writes the answer on stdout is satisfactory.

Rules:

- Only the scheduled reminders use the agent. `/list` in Telegram shows the plain list immediately.
- When you push a "Done" button, the message changes to the plain list.
- If the command fails, writes nothing or runs for more than 3 minutes, the server sends the plain list. The error is in the server log.
- The server cuts the text at 4000 characters. Telegram does not accept more than 4096.
- If `REMINDER_AGENT_PROMPT` is empty, the server uses a built-in prompt: short, plain text, overdue and high priority items first.

Before you set it on the server, try your command in a shell:

```bash
printf 'Write a short reminder.\n\nPending items:\nwork\n  #12 Review PRs (high) due 2026-11-27\n' | <your command>
```

## Add tasks in your own words

When `REMINDER_AGENT_COMMAND` is set, the agent also reads the messages that you send to the bot. Send this:

```
i have to deliver the report in three days, it is for work
```

The agent changes it to the marker syntax, for example `Deliver the report #work @2026-12-01`. Then the bot adds the task and replies:

```
Added #14 Deliver the report (work, high, due 2026-12-01)
```

Rules:

- Make sure that the reply is correct. If it is not correct, edit or delete the task with the CLI.
- The agent can use only the groups that exist. If no group is applicable, the task goes to `master`.
- The agent cannot change data. Its output goes through the same validation as `/add`.
- `/add` does not use the agent. Use it for exact titles, or when the agent does not operate.
- The bot does not read other messages while the agent operates (usually some seconds).
- The prompt for this is built in. `REMINDER_AGENT_PROMPT` is only for the reminders.

## Codex CLI with a cheap model

`gpt-6-luna` is the most efficient model in Codex. OpenAI recommends it for summaries and other short, high-volume tasks. A reminder is that type of task.

Model IDs change. To see the models that you can use, run `codex` and type `/model`.

### The command

```bash
codex exec -m gpt-6-luna -c model_reasoning_effort=low --sandbox read-only --skip-git-repo-check --ephemeral -
```

| Part | Why |
|---|---|
| `exec` | runs one time without a UI. The progress goes to stderr. Only the last answer goes to stdout |
| `-m gpt-6-luna` | the cheap model |
| `-c model_reasoning_effort=low` | less reasoning: faster and fewer tokens. A reminder does not need more |
| `--sandbox read-only` | the agent cannot change files |
| `--skip-git-repo-check` | the server does not run in a Git repository. Without this flag, Codex stops |
| `--ephemeral` | Codex does not keep a session file for each reminder |
| `-` | read the prompt from stdin |

Codex changes quickly. If Codex refuses a flag, run `codex exec --help` and change the command.

### Examples

Default prompt, cheap model:

```bash
REMINDER_AGENT_COMMAND='codex exec -m gpt-6-luna -c model_reasoning_effort=low --sandbox read-only --skip-git-repo-check --ephemeral -'
```

Your own prompt:

```bash
REMINDER_AGENT_PROMPT='You are my strict coach. Write 3 lines maximum. Tell me the one task to do now and why. Plain text, no Markdown.'
```

A prompt in Spanish:

```bash
REMINDER_AGENT_PROMPT='Escribe un recordatorio corto en español. Primero las tareas vencidas. Solo texto, sin Markdown.'
```

A better model for one test (it costs more):

```bash
REMINDER_AGENT_COMMAND='codex exec -m gpt-6.1-sol -c model_reasoning_effort=low --sandbox read-only --skip-git-repo-check --ephemeral -'
```

### Log in to Codex

Do one of these on the server.

**API key** (pay per token). Make a key at <https://platform.openai.com/api-keys>. Then save it in the Codex login:

```bash
printenv OPENAI_API_KEY | codex login --with-api-key
```

**ChatGPT account** (uses your plan limits). The server has no browser, so use the device code:

```bash
codex login --device-auth
```

Open the URL on your phone or computer and type the code.

Both commands save the login in `~/.codex/auth.json`. Keep this file safe: it is the same as a password.

## Run it with Docker Compose

The default image has no shell and no Node.js, so it cannot run Codex. Use [`Dockerfile.codex`](../Dockerfile.codex). It adds `sh` and the Codex CLI.

1. In `docker-compose.yml`, change `build: .` and add a volume for the Codex login:

   ```yaml
   services:
     shttodo:
       build:
         context: .
         dockerfile: Dockerfile.codex
       # ...the other lines do not change
       volumes:
         - shttodo-data:/data
         - codex-home:/root/.codex

   volumes:
     shttodo-data:
     codex-home:
   ```

2. In `.env`, set the command. Use single quotes. If the command contains `$`, type `$$`, because Docker Compose replaces `$`.

   ```bash
   REMINDER_AGENT_COMMAND='codex exec -m gpt-6-luna -c model_reasoning_effort=low --sandbox read-only --skip-git-repo-check --ephemeral -'
   ```

3. Start the server and log in to Codex one time. The login stays in the `codex-home` volume:

   ```bash
   docker compose up -d --build
   docker compose exec shttodo codex login --device-auth
   ```

4. Do a test of the command in the container:

   ```bash
   docker compose exec shttodo sh -c 'printf "Write a short reminder.\n\nPending items:\nwork\n  #12 Review PRs\n" | codex exec -m gpt-6-luna -c model_reasoning_effort=low --sandbox read-only --skip-git-repo-check --ephemeral -'
   ```

To use a fixed Codex version, set the build argument. Run `npm view @openai/codex version` to get the latest version number:

```yaml
    build:
      context: .
      dockerfile: Dockerfile.codex
      args:
        CODEX_VERSION: "<version>"
```

## Other agents

The contract is: prompt on stdin, message on stdout. Examples:

```bash
# Claude Code
REMINDER_AGENT_COMMAND='claude -p --model haiku'

# Any script
REMINDER_AGENT_COMMAND='/data/my-reminder.sh'
```

Each agent CLI must be in the image. Copy `Dockerfile.codex` and change the `npm install` line.
