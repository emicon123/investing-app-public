# ADR-107: Phone Notification for the Morning News Report (ntfy, Sent by the Host Cron Script)

**Date:** 2026-10-26
**Status:** Superseded by ADR-108 (Web Push sent by the app itself, task 172). Implemented under task 171,
then replaced: the outcome table and copy below carry over unchanged into ADR-108.
**Scope:** main app only, host side: `infra/news/news-research.sh`, `infra/.env.example`,
`infra/news/README.md`. No backend, OpenAPI, DB or frontend change. portfolio-b and portfolio-c have no news job.
**Related:** task 171, ADR-041 (the host cron news job), ADR-079 (its current scope and dedup).
ADR-041's file is not in `docs/adr/`, although ADR-064, ADR-077, ADR-079 and `infra/news/README.md`
reference it. This ADR does not recreate it. `docs/architecture/06-data-flow.md` §6 describes the job.

## Context

The morning news report is produced by the host cron job `infra/news/news-research.sh` (06:15
Europe/Warsaw on the Pi, outside Docker): `GET /news/assets`, then `claude -p` with WebSearch, then
`POST /news/ingest`. Today the owner learns the result only by opening `/news`. A failed night
(expired `claude` auth, invalid JSON, ingest error) or an empty night is silent, and the details
exist only in `infra/news/news-research.log` on the Pi.

The owner wants a phone notification each morning saying the report is ready. Constraints:
cost-free and open-source only (AGENTS.md), and the job runs on the Pi host, not in Docker. The
script is the only component that sees every outcome: success, no news, no assets, and every
failure.

## Decision

1. **Channel: ntfy** (https://ntfy.sh, Apache-2.0/GPLv2, free public server, Android and iOS apps,
   no account). The script sends one HTTPS `POST` per run.
2. **Opt-in through `infra/.env`.** Without `NTFY_TOPIC` the job behaves exactly as before.

   | Variable | Meaning |
   | --- | --- |
   | `NTFY_TOPIC` | Unset or empty means off: the script logs `notifications disabled` once and carries on. On the public server the topic name is a shared secret (anyone who knows it can read and publish), so it must be random, e.g. `investing-news-$(openssl rand -hex 12)`. |
   | `NTFY_SERVER` | Default `https://ntfy.sh`. A self-hosted ntfy on the Pi may replace it later. |
   | `NTFY_TOKEN` | Optional access token, sent as `Authorization: Bearer …`, for a protected topic or a self-hosted server. |
   | `NEWS_APP_URL` | Optional click-through target, e.g. `https://<machine>.<tailnet>.ts.net/news` (the SPA route is `/news`, `apps/main/frontend-web/src/lib/routes.ts`). Unset means no click action. |

3. **Exactly one notification per run**, from every exit path after `infra/.env` is loaded:

   | Outcome | title | message | priority | tags |
   | --- | --- | --- | --- | --- |
   | ingest succeeded | `Raport z wiadomościami gotowy` | `Nowe wiadomości: <N> (<YYYY-MM-DD>)` | 3 (default) | `newspaper` |
   | claude returned 0 items | `Raport z wiadomościami gotowy` | `Brak istotnych wiadomości na dziś (<YYYY-MM-DD>)` | 2 (low) | `newspaper` |
   | 0 assets to research | `Raport z wiadomościami gotowy` | `Brak aktywów do analizy (<YYYY-MM-DD>)` | 2 (low) | `newspaper` |
   | any failure | `Raport z wiadomościami — błąd` | `Nie udało się wygenerować raportu (<YYYY-MM-DD>). Szczegóły: infra/news/news-research.log na Pi.` | 4 (high) | `warning` |

   `<N>` is the script's `item_count`: the items Claude returned and the script posted.
   `<YYYY-MM-DD>` is the Europe/Warsaw run date (`news_date`). `click` is `NEWS_APP_URL` when set.
4. **Privacy.** A notification never contains the failure detail, headlines, tickers, raw Claude
   output, HTTP bodies or any token. The topic lives on a third-party server (ntfy.sh caches
   messages for about 12 h). Failure details stay in `news-research.log` only, as today.
5. **Best-effort.** The publish uses `curl --max-time 10`. A failed publish is logged
   (`ntfy notify failed …`) and swallowed. The job's exit code does not change: 0 on the success
   paths, 1 on failure.
6. **Unexpected aborts notify too.** `set -euo pipefail` can end the script outside `fail()`, for
   example when a python step crashes. An `EXIT` trap sends the failure notification when the exit
   status is non-zero and nothing has been sent yet in this run. `fail()` and the three success
   paths mark the run as sent, so no run notifies twice.
7. **JSON publishing.** `POST ${NTFY_SERVER}` (the root URL) with the body
   `{"topic","title","message","priority","tags","click"}`, built with `python3 json.dumps`
   (already a dependency). Header-style publishing is not used, because the Polish characters in
   the titles do not survive raw HTTP headers.
8. **Deploy.** A push to `main` that changes `infra/news/news-research.sh` runs `deploy-pi.yml`
   (`infra/*` maps to the main stack), which pulls the canonical checkout
   (`~/Claude-Code/investing-app`) that the crontab runs the script from. No crontab
   change. The owner adds the variables to the Pi's `infra/.env` and subscribes the ntfy phone app
   to the topic by hand, following `infra/news/README.md`.

**Implementation notes for DevOps** (these follow from the decisions, they add none):
- The script currently computes `news_date` only in step 4, just before ingest. Every message
  needs it, so compute it once, right after `infra/.env` is sourced.
- In ntfy's JSON format `priority` is an integer and `tags` is an array (`["newspaper"]`). Leave
  `click` out when `NEWS_APP_URL` is unset. Treat a non-2xx reply as a failed publish. Never log
  `NTFY_TOKEN` or the request headers.
- Under `set -e` an unguarded failing command inside the `EXIT` trap replaces the exit code (a
  0 exit became 1 in a test). Guard every command in `notify` and the trap with `|| …`, and end the
  trap with `exit "$rc"`, where `rc` is `$?` captured on entry.
- Failures before `infra/.env` is sourced cannot notify, because the topic is unknown. The cron
  `>>` redirect still logs them. The `prompt.md` check may move below the `source` so that it
  notifies too.

## Pattern evaluation

| Candidate | Verdict |
| --- | --- |
| One `notify` shell function (a facade over `python3` + `curl`) | Adopted. One place builds the body, applies the timeout and swallows errors. Call sites pass only priority, tags, title and message. |
| Strategy (pluggable notification channels) | Rejected (YAGNI). There is one channel, and `NTFY_SERVER` already covers self-hosting. |
| Retry or queue | Rejected. The publish is best-effort, and ntfy itself stores the message for the phone. |

## Alternatives considered

- **Web Push / PWA.** Rejected. It needs VAPID keys, a service worker, a subscription store in the
  DB and new backend code. iOS delivers only to a PWA installed on the home screen. That is far
  more surface than one message a day.
- **Telegram bot.** Rejected. It is a proprietary service and needs a bot token and a chat id.
- **E-mail.** Rejected. It needs SMTP credentials on the Pi and does not arrive instantly on a phone.
- **Sending from the Go backend after `POST /news/ingest`.** Rejected. The backend sees only
  successful ingests. It never learns about a failed night (the script stops before ingest) or an
  empty night (no ingest call is made), and those are the outcomes the owner most needs to hear
  about. It would also need new outbound config and code in the backend, while the script already
  has every fact.

## Consequences

- **A third-party server holds the topic.** ntfy.sh caches each message for about 12 h, and anyone
  who learns the topic name can read it or publish to it. Mitigations: a random topic, content
  limited to a count and a date (decision 4), an optional `NTFY_TOKEN`, and a self-hosted server
  through `NTFY_SERVER`. A forged message can only mislead about whether the report exists.
  `/news` shows the truth.
- **Best-effort, so silence proves nothing.** If the Pi is off at 06:15 the job never runs and no
  notification arrives. A missing morning notification therefore means "check the Pi". Failures
  before `infra/.env` is loaded do not notify either.
- **`<N>` counts posted items, not stored rows.** The backend can store fewer. It skips exact repeat
  headlines (ADR-079), and `uq_asset_news_ticker_date` keeps one row per ticker per day. "Items
  researched" is enough for "the report is ready".
- **The default is off.** Without `NTFY_TOPIC` the job adds only one log line. portfolio-b and portfolio-c are
  not affected. No backend, OpenAPI, DB or frontend change. News content stays web-only (ADR-041),
  and the notification only points at `/news`.
- **Living docs updated in this change:** `docs/architecture/01-system-context.md` (host cron
  external calls, the network table, the system context diagram) and
  `docs/architecture/06-data-flow.md` §6 (the sequence diagram and key properties). DevOps adds the
  setup steps to `infra/news/README.md`. This ADR keeps the outcome table, the reasoning and the
  rejected alternatives.
