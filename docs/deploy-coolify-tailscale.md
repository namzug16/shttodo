# Deploy with Coolify and Tailscale

This guide deploys shttodo on a VPS that runs Coolify. The API is private: only devices on your tailnet can reach it. Other public apps on the same Coolify instance do not change.

How it works:

```
CLI (tailnet device) --HTTPS--> tailscale serve (VPS) --> 127.0.0.1:<port> --> shttodo container
shttodo container --long polling--> Telegram API (outbound only)
```

The container publishes its port on `127.0.0.1` only, and Coolify gives it no domain. Tailscale Serve is the only way in.

## Placeholders

| Placeholder | Meaning |
|---|---|
| `<vps>` | the Tailscale machine name of the VPS |
| `<tailnet>` | your tailnet DNS name, for example `tail1234` |
| `<port>` | a free localhost port on the VPS, for example `3101` |
| `<repo-url>` | the Git URL of your shttodo repository |

## 1. Prepare the secrets

Get `SHTTODO_API_TOKEN`, `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID`. Refer to [Get the tokens](../README.md#get-the-tokens).

## 2. Install Tailscale on the VPS

```bash
curl -fsSL https://tailscale.com/install.sh | sh
sudo tailscale up --accept-dns=false
```

- `--accept-dns=false` keeps the VPS DNS settings. The public apps continue to resolve names as before.
- Do not enable exit node, subnet routes (`--accept-routes`) or Tailscale SSH unless you need them.
- In the Tailscale admin console, enable **MagicDNS** and **HTTPS certificates**. `tailscale serve --https` needs both.

## 3. Create the Coolify resource

1. In Coolify, add a new resource from your Git repository (`<repo-url>`). Select the **Docker Compose** build pack. Coolify uses the `docker-compose.yml` in the repository.
2. Set the environment variables:

   | Variable | Value |
   |---|---|
   | `SHTTODO_API_TOKEN` | the token from step 1 |
   | `TELEGRAM_BOT_TOKEN` | the bot token from step 1 |
   | `TELEGRAM_CHAT_ID` | the chat ID from step 1 |
   | `SHTTODO_HOST_PORT` | `<port>` (default `3101`) |
   | `REMINDER_INTERVAL_MINUTES` | must divide 1440, default `60` |
   | `REMINDER_ACTIVE_HOURS` | inclusive hours, default `09-21` |
   | `TZ` | your IANA timezone, for example `Europe/Berlin` |

3. **Do not set a domain** for the service. A domain makes Coolify's proxy route public traffic to the container.
4. Deploy. The logs must show `shttodo listening on :8080`.

The compose file maps the port as `127.0.0.1:<port>:8080`. Do not change it to `<port>:8080`. Docker publishes ports past `ufw`, so a port without `127.0.0.1` is public even if the firewall blocks it.

## 4. Expose the API with Tailscale Serve

```bash
sudo tailscale serve --bg --https=443 http://127.0.0.1:<port>
tailscale serve status
```

The API is now at `https://<vps>.<tailnet>.ts.net`. `--bg` keeps the configuration after a reboot.

If another service already uses Serve on port 443, use another HTTPS port, for example `--https=8443`. Then the URL is `https://<vps>.<tailnet>.ts.net:8443`.

## 5. Check that the API is private

Do all of these checks. Do not skip them.

1. On the VPS, the port must listen on localhost only:

   ```bash
   ss -ltn | grep <port>
   # expected: 127.0.0.1:<port>, not 0.0.0.0:<port>
   ```

2. From a device that is **not** on the tailnet (for example a phone with Wi-Fi off and Tailscale off), this must fail or time out:

   ```bash
   curl -m 5 http://<vps-public-ip>:<port>/api/groups
   ```

3. From a device on the tailnet, this must return JSON:

   ```bash
   curl -H "Authorization: Bearer <api-token>" https://<vps>.<tailnet>.ts.net/api/groups
   ```

## 6. Configure the CLI

On each machine that uses shttodo (it must be on the tailnet):

```bash
git clone <repo-url> && cd shttodo && go install .
export SHTTODO_SERVER_URL=https://<vps>.<tailnet>.ts.net
export SHTTODO_API_TOKEN=<api-token>
shttodo group list
```

Put the two `export` lines in your shell profile. AI agents read the same variables.

## 7. Back up the database

The database is one SQLite file on the `shttodo-data` Docker volume. Coolify adds a prefix to the volume name. Find it:

```bash
docker volume ls | grep shttodo-data
```

Use one of these:

- VPS snapshots from your hosting provider.
- A daily cron job on the host (needs the `sqlite3` package on the host). `.backup` is safe while the server runs:

  ```bash
  sqlite3 /var/lib/docker/volumes/<volume-name>/_data/shttodo.db ".backup /root/backups/shttodo-$(date +%F).db"
  ```

## Option: one Tailscale identity per app

If you need a separate tailnet hostname or separate access rules for each private app, run a Tailscale sidecar container in the compose file (`tailscale/tailscale` image, with the app on `network_mode: service:tailscale`). This costs one more container per app. For one private app, Tailscale Serve on the host is simpler.
