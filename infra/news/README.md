# Daily asset news — host cron job (ADR-041)

See `~/Claude-Code/investing-app/docs/adr/ADR-041-daily-asset-news-host-cron-claude-cli.md`
for the full architecture rationale.

## What this is

A job that runs at **06:15 Europe/Warsaw** and researches today's notable news for
each MAIN-portfolio asset that has its own distinguishable,
individually-researchable narrative (`companyBacked = true`, ADR-064,
semantics broadened by ADR-079 — excludes only cash/FX rows and basket/index/
commodity ETF vehicles). This now **includes** leaderless protocol-level
assets like BTC-USD, which has its own asset-intrinsic news (protocol
upgrades/proposals, governance debates, hard forks, hash-rate/on-chain
activity) even with no company or foundation behind it. Each asset the
endpoint returns also carries `recentHistory` (ADR-079) — its own recently
reported headlines — so the prompt can avoid re-reporting an ongoing story
night after night. The job uses the host-installed, already-authenticated
`claude` CLI (`~/.local/bin/claude`) with the `WebSearch` tool —
**not** a paid Anthropic API call. It runs directly on the Raspberry Pi host
(NOT inside Docker), because the backend container has no LLM client and the
project is cost-free-only.

Running at 06:15 rather than 00:00 is deliberate: it keeps the research and
the `newsDate` stamped on the item (today's date, Europe/Warsaw, at the
moment the script runs) safely clear of the midnight boundary. Running in
the early morning rather than late at night has a second benefit too:
by 06:15 the prior day's US market session has closed, so the research
captures the full prior day's developments — including the US close and
any overnight/Asian-session news — rather than potentially running before
the US session has even finished for the day.

Flow:

1. `GET {API_BASE}/news/assets` (with `X-Service-Token` header) → list of
   `{ticker, assetName, recentHistory}` to research (`recentHistory` is that
   asset's own recently reported news, ADR-079 — used by the prompt to avoid
   re-reporting an ongoing story).
2. Combine that list with the prompt template in `prompt.md`, and run
   `claude -p "<prompt>" --output-format json --allowedTools "WebSearch"`.
3. Extract the `.result` field from the JSON wrapper, strip any stray
   markdown code fences, and validate it's a JSON array of news items
   (`ticker`, `assetName`, `headline`, `summary`, `sentiment`, `sources`).
4. Wrap it as `{"items": [...]}` and `POST {API_BASE}/news/ingest` (with the
   same `X-Service-Token` header).
5. Log every step (timestamps, asset count, ingest result) to
   `news-research.log` in this directory (gitignored — see
   `infra/news/*.log` in the repo's `.gitignore`).
6. Report the run's outcome — exactly once per run, on every exit path that got
   as far as loading `infra/.env` — to `POST {API_BASE}/news/notifications`
   (same `X-Service-Token`); the backend turns it into a Web Push notification
   on every subscribed phone/browser — see *Phone notification* below (ADR-108).

Files:

- `news-research.sh` — the script described above. Executable
  (`chmod +x` already applied).
- `prompt.md` — static prompt template instructing Claude how to research and
  exactly what JSON shape to emit.
- `news-research.log` — created on first run; not committed.

## Prerequisites

- `infra/.env` must exist (copy from `.env.example` if missing) and must
  contain `NEWS_INGEST_TOKEN`, a strong random shared secret:

  ```bash
  openssl rand -hex 32
  ```

  This value **must match** what the backend has been started with (the Go
  backend reads the same `NEWS_INGEST_TOKEN` from `infra/.env` via
  `infra/compose.yml`). If the tokens don't match, `/news/assets` and
  `/news/ingest` will return 401.

- The `claude` CLI must already be logged in / authorized on this host
  (`~/.local/bin/claude`). This script never attempts any
  login flow — if the CLI isn't authorized, the script will fail at the
  `claude -p` step.
- `curl` and `python3` must be on `PATH` (both are used for the HTTP calls
  and JSON extraction/validation; no `jq` dependency).
- The backend's `GET /news/assets` and `POST /news/ingest` endpoints must
  exist and be running (via `docker compose up -d backend-go` from `infra/`).

## Testing manually

Run it by hand from anywhere (it resolves its own paths from
`$BASH_SOURCE`):

```bash
~/Claude-Code/investing-app/infra/news/news-research.sh
```

Check the outcome:

```bash
tail -f ~/Claude-Code/investing-app/infra/news/news-research.log
```

On success, exit code is `0` and the log shows the asset count fetched, the
number of valid news items Claude returned, and the ingest HTTP response. On
any failure (curl error, missing/invalid `NEWS_INGEST_TOKEN`, invalid JSON
from `claude -p`, non-2xx from `/news/ingest`), the script exits non-zero and
prints a clear message to stderr in addition to logging it.

To re-verify end to end once the backend is live:

```bash
curl -H "X-Service-Token: $NEWS_INGEST_TOKEN" http://localhost/api/news/assets
```

should return the MAIN non-currency asset list before you run the full
script.

## Installing the nightly crontab entry

This script is **not** installed into crontab automatically — install it
yourself once you've confirmed a manual run works end to end:

```bash
crontab -e
```

Add this line (runs at 06:15 Europe/Warsaw daily; adjust if the system
timezone differs — `date` on the host should already report Europe/Warsaw):

```cron
15 6 * * * ~/Claude-Code/investing-app/infra/news/news-research.sh >> ~/Claude-Code/investing-app/infra/news/news-research.log 2>&1
```

Note the script already logs internally to the same `news-research.log` file
via timestamped lines; the `>>` redirect in the crontab entry is a safety net
that also captures anything printed to stdout/stderr outside the script's own
logging (e.g. a shell-level error before the script's `set -euo pipefail`
trap engages, or output from `cron` itself if the script path is wrong).

## Phone notification (Web Push, ADR-108)

Every run that got as far as loading `infra/.env` ends with **exactly one**
call to the backend's machine-only `POST {API_BASE}/news/notifications`
(`X-Service-Token`, the same `NEWS_INGEST_TOKEN`). The body is a structured
outcome only — `{"outcome": "READY" | "NO_NEWS" | "NO_ASSETS" | "FAILED",
"newsDate": "YYYY-MM-DD", "itemCount": N}` (`itemCount` only for `READY`).
The backend owns the Polish text and pushes it with standard **Web Push**
(VAPID, end-to-end encrypted) to every browser that turned notifications on
in the app's Settings. No third-party phone app, no account, free. This
replaces the ntfy notification of ADR-107.

One-time setup:

1. Generate a VAPID key pair on the Pi (Go is installed; it prints two
   `.env` lines and never writes a file):

   ```bash
   cd ~/Claude-Code/investing-app/apps/main/backend-go && go run ./cmd/vapidkeys
   ```

2. Add them to `infra/.env` (gitignored; see `infra/.env.example`), plus a
   contact subject, and **remove `NTFY_TOPIC`** (and any other `NTFY_*` /
   `NEWS_APP_URL` line) — nothing reads them any more:

   ```bash
   VAPID_PUBLIC_KEY=<printed>
   VAPID_PRIVATE_KEY=<printed>      # secret
   VAPID_SUBJECT=mailto:<your e-mail>   # keep the mailto: prefix (or use an https: URL)
   ```

   Push is enabled only when all three are set and the subject starts with
   `mailto:` or `https:`. **Rotating the key pair invalidates every
   subscription**: each device re-subscribes by itself on its next visit to
   `/settings`; until then sends to it fail with `403`.
3. Restart the backend so it reads the keys (`docker compose up -d
   backend-go` from `infra/` — the orchestrator's step, after the deploy).
   The backend logs `web push enabled=true` at startup.
4. On the phone (Android, Chrome), open the app over its HTTPS Tailscale
   Funnel URL, go to **`/settings`** → card **"Powiadomienia"** → **"Włącz na
   tym urządzeniu"** and allow notifications when Chrome asks.
5. Tap **"Wyślij testowe powiadomienie"**. "Powiadomienie testowe" should
   appear within seconds; tapping it opens `/news`. A 2xx from the push
   service only proves delivery to Google, not to the screen — if nothing
   shows, check Android's notification/battery settings for Chrome.
6. Optional: Chrome menu → **"Install app"** (or "Add to Home screen") for a
   standalone app window with native-looking notifications.

**iOS:** Web Push works only on iOS/iPadOS 16.4+ from the app **installed to
the home screen** (Safari → Share → "Add to Home Screen"), then enabled from
`/settings` inside that installed app. Not optimised for.

What the four notifications mean (Polish, as they appear on the phone; `N` is
the number of items Claude returned and the script posted, the date is the
Europe/Warsaw run date):

| `outcome` | Title | Message | Urgency |
| --- | --- | --- | --- |
| `READY` | `Raport z wiadomościami gotowy` | `Nowe wiadomości: N (YYYY-MM-DD)` | normal |
| `NO_NEWS` | `Raport z wiadomościami gotowy` | `Brak istotnych wiadomości na dziś (YYYY-MM-DD)` | low |
| `NO_ASSETS` | `Raport z wiadomościami gotowy` | `Brak aktywów do analizy (YYYY-MM-DD)` | low |
| `FAILED` | `Raport z wiadomościami — błąd` | `Nie udało się wygenerować raportu (YYYY-MM-DD). Szczegóły: infra/news/news-research.log na Pi.` | high |

A newer news notification replaces the previous one on the phone (same tag),
and an undelivered one expires at the push service after 12 h.

Things to know:

- **Privacy.** The script sends only the outcome, a count and the date —
  never headlines, tickers, Claude output, HTTP bodies or the failure detail.
  The payload is encrypted end to end to the browser. Failure details stay in
  `news-research.log`.
- **Best-effort.** The call uses `curl --max-time 30 --retry 2 --retry-delay 5
  --retry-connrefused` (rides out a backend restart during a deploy). The log
  line says what happened: `push notify sent (… HTTP 200, attempted=… delivered=…
  pruned=… failed=…)`, `push not configured (… HTTP 503)` when the `VAPID_*`
  keys are missing, or `push notify failed (…)`. The job's exit code is never
  changed by the notification.
- **What silence means (ADR-108 §3).** No notification by about 07:00 means
  something on the Pi did not run: the Pi was off, the cron entry is missing,
  `infra/.env` is missing, or the backend was down while the job ran (a down
  backend cannot push — there is no third party any more). Open `/news`; if
  today's date is missing, check `news-research.log` on the Pi.
- To turn it off for one device, use **"Wyłącz na tym urządzeniu"** in
  `/settings`; to turn it off entirely, remove the three `VAPID_*` lines and
  `up -d backend-go`.

## Known limitations / gotchas

- If the Pi is rebooted or powered off at 06:15, that morning's run is simply
  skipped — no backfill, no catch-up job (same accepted-gap precedent as the
  Historia nightly snapshot, which runs on its own schedule).
- `claude -p` CLI flags or `WebSearch` tool availability can change between
  Claude Code releases; this script is isolated from the backend so fixing it
  never requires a backend redeploy.
- The script intentionally does not retry on failure — a failed night simply
  produces no rows for that day; cron will try again at the next scheduled
  time.
