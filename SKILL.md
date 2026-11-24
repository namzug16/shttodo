---
name: shttodo
description: Manage the user's tasks, groups and daily habits with the shttodo CLI. Use when the user asks to add, list, change, complete or delete todos, or to track daily habits.
---

# shttodo

shttodo is the user's single-user task manager. The backend stores tasks and daily habits, and sends hourly Telegram reminders until each item is done. You manage the data through the `shttodo` CLI.

## Setup

The CLI needs two environment variables:

- `SHTTODO_SERVER_URL`: the backend URL, for example `https://host.tailnet.ts.net`
- `SHTTODO_API_TOKEN`: the API token

If a command returns `config_missing`, ask the user for the missing value. Do not guess it.

## Rules for agents

1. Always add `--json`. Parse stdout. In JSON mode, errors are also on stdout.
2. Run `shttodo list --json` before `add`, so you do not create duplicates.
3. Use IDs from command output. Never guess IDs.
4. Put context the user will need later in `--notes` (links, reasons, next steps).
5. Successful commands return the full object. Do not run `show` after a change.

## Tasks

```
shttodo add <title> [--group G] [--priority low|normal|high] [--due YYYY-MM-DD] [--notes N]
shttodo list [--group G] [--all]     # --all includes done tasks
shttodo show <id>
shttodo edit <id> [--title T] [--group G] [--priority P] [--due D] [--notes N]
shttodo done <id>
shttodo reopen <id>                  # done -> pending
shttodo delete <id>                  # permanent
```

- `edit` changes only the flags you give. An empty value clears an optional field: `--due ""`, `--notes ""`.
- `--notes` replaces all notes. To add to notes, read them first and write the full new text.
- Move a task to another group with `edit <id> --group <name>`.
- The default group is `master`. The default priority is `normal`.
- `list` order: group name, then priority (high first), then due date (no due date last), then ID.

Task JSON:

```json
{"id":12,"title":"Review PRs","group":"work","priority":"high","due":"2026-12-01","notes":"","status":"pending","created_at":"2026-11-24T15:04:05Z","completed_at":null}
```

`due` and `notes` are `""` when not set. `status` is `pending` or `done`.

## Groups

```
shttodo group create <name>          # 1-32 characters: a-z, 0-9, -
shttodo group list
shttodo group rename <old> <new>
shttodo group delete <name>          # fails while the group has tasks (done tasks too)
```

`master` cannot be renamed or deleted.

Group JSON:

```json
{"name":"work","pending_task_count":3,"total_task_count":7}
```

## Habits (daily todos)

Habits repeat every day. Each day is either done or missed. The backend keeps the history.

```
shttodo habit add <title> [--notes N]
shttodo habit list                   # today's status and streaks
shttodo habit show <id> [--since YYYY-MM-DD]   # adds completion_dates, default: last 30 days
shttodo habit done <id> [--date YYYY-MM-DD]    # default: today; future dates are rejected
shttodo habit undo <id> [--date YYYY-MM-DD]
shttodo habit edit <id> [--title T] [--notes N]
shttodo habit delete <id>            # permanent, deletes the history too
```

Habit JSON:

```json
{"id":3,"title":"Gym","notes":"","created_at":"2026-11-24T15:04:05Z","done_today":true,"current_streak":5,"longest_streak":12,"total_days_done":40,"completion_dates":["2026-11-20","2026-11-24"]}
```

`completion_dates` appears only in `habit show`. The current streak stays alive until the end of today, so a habit done yesterday but not yet today still has its streak.

## Errors

Every error exits with code 1. In JSON mode:

```json
{"error":{"code":"not_found","message":"task 99 does not exist"}}
```

| code | meaning | what to do |
|---|---|---|
| `config_missing` | an environment variable is not set | ask the user for the value |
| `server_unreachable` | the backend does not answer | tell the user; check `SHTTODO_SERVER_URL` and Tailscale |
| `unauthorized` | wrong API token | ask the user to check `SHTTODO_API_TOKEN` |
| `not_found` | the task, habit, group or route does not exist | run `list` and use a real ID or name |
| `validation_failed` | bad input | read `message`, fix the input |
| `conflict` | the name already exists, or a group still has tasks | read `message` |

## Telegram

The user gets a reminder each interval during active hours. It shows the habits not yet done today, then the pending tasks by group, with a done button for each item. You do not need to send reminders.
