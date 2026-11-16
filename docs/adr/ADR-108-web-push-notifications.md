# ADR-108: Web Push Notifications Sent by the App (Replaces ntfy)

**Date:** 2026-10-26
**Status:** Accepted. Implemented under task 172 (deployed 2026-10-28, verified on the owner's Android phone 2026-10-29).
**Supersedes:** ADR-107 (ntfy phone notification for the morning news report).
**Scope:** main app only: `apps/main/backend-go` (new `internal/push` package, one new news
endpoint, Flyway `V204`), `apps/main/frontend-web` (service worker, manifest, Settings card),
`infra/news/news-research.sh`, `infra/.env.example`, `infra/compose.yml`, `infra/nginx/nginx.conf`,
`infra/news/README.md`. portfolio-b and portfolio-c are not changed.
**Design and diagrams:** `docs/architecture/diagrams/172-web-push.md`.

## Context

ADR-107 (task 171, deployed 2026-10-26, `597a074`) sends the morning news outcome to the owner's
phone through ntfy.sh. It works, but it needs a third-party phone app. The owner does not want
another app: the notification must come from the web app itself, the way websites notify. That is
the standard Web Push stack: the Push API and a service worker in the browser, VAPID
(RFC 8292) to identify the sender, and RFC 8291 payload encryption. The browser vendor runs the
push service (Google FCM for Chrome on Android, Mozilla autopush, Apple). It is free and needs no
account. ADR-107 rejected Web Push as "far more surface". The owner has now chosen that surface.

Constraints: cost-free and open-source only; runs on the Pi; secrets only in `infra/.env`. The app
is public at one HTTPS origin through Tailscale Funnel, and that origin serves three apps: main at
`/`, portfolio-b at `/portfolio-b/`, portfolio-c at `/portfolio-c/`.

## Decision

### 1. Channel and library

The Go backend sends Web Push itself, with **`github.com/SherClockHolmes/webpush-go` v1.4.0**.

| Check (2026-10-26) | Result |
| --- | --- |
| License | MIT (GitHub license API, `LICENSE` in the repo) |
| Maintenance | not archived; last commit 2026-04-22; v1.4.0 tagged 2025-01-02; 457 stars, 10 open issues |
| `go.mod` of v1.4.0 | `github.com/golang-jwt/jwt/v5 v5.2.1`, `golang.org/x/crypto v0.31.0`. Both are already in our graph (`jwt/v5 v5.3.1`), so MVS keeps our newer versions. No cgo. |
| What it covers | RFC 8291 `aes128gcm` encryption, the VAPID JWT and the `vapid t=…, k=…` header, the `TTL` / `Urgency` / `Topic` headers, `GenerateVAPIDKeys()` |

Writing RFC 8291 by hand is not allowed (task decision 1). Library gotchas:
- `Options.Subscriber` takes the bare e-mail. The library adds `mailto:` itself unless the value
  starts with `https:`. The adapter therefore strips a leading `mailto:` from `VAPID_SUBJECT`.
- `SendNotificationWithContext` returns the raw `*http.Response`. The adapter must close the body
  on every path.

### 2. Who triggers the push: the script, through the backend (question 1, confirmed)

The script is still the only component that sees every outcome, including the empty nights and
the failed ones. It reports the outcome to a new service-token endpoint, and the backend fans it
out to every stored subscription.

- `POST /api/news/notifications` (operation `notifyNewsReport`, `X-Service-Token`, machine-only)
  with the body `{ "outcome": "READY" | "NO_NEWS" | "NO_ASSETS" | "FAILED", "newsDate":
  "YYYY-MM-DD", "itemCount": N }`. `itemCount` is required, and must be at least 1, only for
  `READY`.
- **The body is a structured outcome, not free text.** The backend holds the Polish copy, so the
  privacy rule (decision 5) is enforced in code. A token holder cannot push arbitrary text, and the
  copy exists in one place.
- The script calls it from the same four places, plus the `EXIT` trap, that called ntfy. The
  ADR-107 rules stay: exactly one call per run, best-effort, and the job's exit code never changes.
  It uses `curl --max-time 30 --retry 2 --retry-delay 5 --retry-connrefused`, so a backend restart
  during a deploy does not lose the morning message. If a retry follows a partial fan-out, the
  notification `tag` (decision 5) replaces the earlier copy on the phone, so it does not stack.
- Generic for the next consumer: `push.Service.Broadcast(ctx, Notification)` is the reusable part.
  A future in-process consumer (for example bot signals) calls it directly, with no new HTTP
  surface. `/news/notifications` is only the news use case's adapter. No other consumer is built
  now (YAGNI).

### 3. The silent-night gap: accepted, no overdue check (question 2)

With ntfy, the script could still report a failure when the backend was down. With Web Push it
cannot, because a down backend cannot push. A backend "report overdue" check at about 07:30 would
cover only one narrow case: the backend down at about 06:15 but back up by 07:30. It would need a
new scheduler, a record of notify calls, and Warsaw-time edge cases. Rejected (YAGNI).

**What silence now means:** no notification by about 07:00 means something on the Pi did not run
(the Pi was off, the cron entry is missing, or the backend was down while the job ran). Open
`/news`. If today's date is missing, check `infra/news/news-research.log` on the Pi. This is the
same instruction ADR-107 gave ("check the Pi"), and now one more cause leads to it. Revisit the
decision if two silent mornings ever happen while the Pi is healthy.

### 4. Subscription model (question 3)

One table, `push_subscriptions` (Flyway `V204__push_subscriptions.sql`, exact DDL in
`docs/architecture/04-db-schema.md` § Table `push_subscriptions`):

| Column | Meaning |
| --- | --- |
| `id` | `BIGSERIAL` PK |
| `user_email` | the subscribing user's JWT principal. Needed because two Google accounts are allowlisted and the test button must reach only the caller's devices. |
| `endpoint` | push-service URL, `UNIQUE`, `https://` only, at most 2048 chars. It is a capability URL: treat it as a secret. |
| `p256dh`, `auth` | the subscription's public key and auth secret (base64url) |
| `device_label` | short label derived on the server from `User-Agent`, e.g. `Android · Chrome` |
| `created_at`, `updated_at` | `updated_at` is refreshed by each re-subscribe (upsert) |
| `last_success_at` | last 2xx from the push service, `NULL` until the first delivery |

- **Several devices, one user or two.** Subscribing is an upsert on `endpoint`: a re-subscribe
  refreshes the keys, label, owner and `updated_at`. A news notification goes to every row. The
  test notification goes only to rows whose `user_email` is the caller.
- **Endpoint allowlist (SSRF guard).** The backend POSTs to a URL that the browser supplied. A
  subscribe call is rejected with `422` unless the scheme is `https` and the host is, or ends in,
  `fcm.googleapis.com`, `push.services.mozilla.com`, `push.apple.com` or `notify.windows.com`.
  The list is a constructor argument of the push service, so tests can widen it to their stub.
- **Retention.** A row lives until the browser unsubscribes (`DELETE`) or the push service answers
  `404`/`410`, which deletes it during the send. There is no time-based pruning.
- **How each send result is handled. There is no retry queue (ADR-107's best-effort stance).**

  | Push-service answer | Action | Counted as |
  | --- | --- | --- |
  | `2xx` (normally `201`) | set `last_success_at = now()` | `delivered` |
  | `404`, `410` | delete the row | `pruned` |
  | `429`, `5xx`, network error or timeout | log the status, keep the row | `failed` |
  | `401`, `403` (VAPID rejected, for example after a key rotation) | log WARN, keep the row | `failed` |
  | `400`, `413`, anything else | log ERROR, keep the row | `failed` |

  The sends run one after another, with a 5 s timeout each (the owner has a handful of devices).
  Every call returns `{attempted, delivered, pruned, failed}`.
- **Logging.** Never log the `endpoint`, `p256dh`, `auth` or the VAPID private key at any level.
  Log the subscription `id`, the endpoint's host (for example `fcm.googleapis.com`), the HTTP
  status and the counts.

### 5. Payload and copy (task decision 4)

The encrypted payload is the JSON `{ "title", "body", "url", "tag" }`. The service worker shows
it. The copy is ADR-107's, word for word:

| `outcome` | `title` | `body` | `Urgency` |
| --- | --- | --- | --- |
| `READY` | `Raport z wiadomościami gotowy` | `Nowe wiadomości: <N> (<YYYY-MM-DD>)` | `normal` |
| `NO_NEWS` | `Raport z wiadomościami gotowy` | `Brak istotnych wiadomości na dziś (<YYYY-MM-DD>)` | `low` |
| `NO_ASSETS` | `Raport z wiadomościami gotowy` | `Brak aktywów do analizy (<YYYY-MM-DD>)` | `low` |
| `FAILED` | `Raport z wiadomościami — błąd` | `Nie udało się wygenerować raportu (<YYYY-MM-DD>). Szczegóły: infra/news/news-research.log na Pi.` | `high` |
| test button | `Powiadomienie testowe` | `Powiadomienia działają na tym urządzeniu.` | `normal` |

- `url` is the relative path `/news`. The test notification also uses `/news`, because the owner's
  verification step is "tap opens /news". `NEWS_APP_URL` goes away: the worker resolves the path
  against its own origin.
- `tag` is `news-report` for news and `push-test` for the test. A notification with the same tag
  replaces the previous one, with `renotify: true`, so yesterday's report notice does not pile up.
- `TTL` is 43 200 s (12 h) for news and 300 s for the test. After that the push service drops a
  message that has not been delivered. The `Topic` header is `news-report`, so a newer undelivered
  news message replaces an older one at the push service.
- **Privacy is unchanged.** A payload carries a count and a date, never headlines, tickers,
  failure detail, HTTP bodies or tokens. It is end-to-end encrypted to the browser, so the push
  service cannot read it. That is a privacy improvement over ntfy.sh.

### 6. Service worker, scope and manifest (question 4)

- **`/sw.js`**, served from the root. It is a plain hand-written file in
  `apps/main/frontend-web/public/`, so Vite copies it unhashed to `dist/` and it is not bundled.
  Its scope is `/`, because the main app's routes (`/portfolio`, `/news`, …) share no other prefix.
- **The worker has exactly three listeners: `install` (`self.skipWaiting()`), `push` and
  `notificationclick`. It has no `fetch` listener**, and it neither caches nor calls
  `clients.claim()`. Its root scope also covers `/portfolio-b/` and `/portfolio-c/`, but without a `fetch`
  listener the browser sends those pages' requests straight to the network. The worker never sees
  them, so it cannot interfere with the sibling apps. A frontend test asserts that the file
  registers no `fetch` listener (handoff, step F2).
- `notificationclick` closes the notification, focuses an open tab of this origin that is already
  on `data.url`, or else opens `data.url`. It accepts only a same-origin relative path starting
  with `/`, and falls back to `/news` otherwise.
- **Manifest:** `/manifest.webmanifest`, with `name` `Inwestycje`, `start_url` `/portfolio`,
  `scope` `/`, `display` `standalone`, and PNG icons at 192 and 512 px (Chrome's install
  criteria) under `/icons/`. `index.html` links it and sets `theme-color`. Notifications use
  `/icons/icon-192.png` as the `icon` and a monochrome `/icons/badge-96.png` as the `badge`, which
  Android shows in the status bar. The installed app's scope `/` includes the sibling paths. That
  is harmless: they open inside the same app window.
- **nginx** adds exact `location = /sw.js` and `location = /manifest.webmanifest` blocks with
  `Cache-Control: no-cache`, `try_files $uri =404` (a missing file must 404, not fall back to
  `index.html`), and the explicit types `application/javascript` and `application/manifest+json`.
  The `/api/push/` prefix gets a proxy block, like the other `/api/*` prefixes. The existing
  `location /api/news` prefix already covers `/api/news/notifications`. **CSP does not change:**
  the worker and the manifest are `'self'`, and the push service talks to the browser, not to the
  page.
- **iOS:** Web Push reaches iOS 16.4+ only from the app installed to the home screen. This is
  documented in the README and not optimised for.

### 7. HTTP contract and auth (question 5)

| Method and path | operationId | Auth (`smartAuth`) | Unconfigured (`VAPID_*` missing) |
| --- | --- | --- | --- |
| `GET /api/push/config` | `getPushConfig` | bearer JWT (default branch) | `200 {enabled:false}`, no key |
| `POST /api/push/subscriptions` | `upsertPushSubscription` | bearer JWT | `503 PUSH_NOT_CONFIGURED` |
| `DELETE /api/push/subscriptions` (body `{endpoint}`) | `deletePushSubscription` | bearer JWT | works, `204` |
| `POST /api/push/test-notifications` | `sendTestPushNotification` | bearer JWT | `503 PUSH_NOT_CONFIGURED` |
| `POST /api/news/notifications` | `notifyNewsReport` | `X-Service-Token` only (added to `isMachineOnly`) | `503 PUSH_NOT_CONFIGURED` |

- `401`: missing or invalid credential of the kind the route needs. A bearer JWT sent to
  `/news/notifications` gets `401`, exactly like `/news/ingest`. `403` stays the shared "not in the
  allowlist" response. `422`: validation, which covers a malformed body, an endpoint not on the
  allowlist, a missing or extra `itemCount`, and an unknown `outcome`. `500`: a database failure.
  Push-service failures are never an HTTP error. They show up as `failed` in the report.
- The `DELETE` carries the endpoint in a JSON body, not in the path or a query string, because
  the endpoint is a capability and URLs end up in nginx access logs. `DELETE` of an unknown
  endpoint is still `204` (idempotent).
- `deviceLabel` is never sent by the client. The server derives it from `User-Agent`.

**Configuration.** `VAPID_PUBLIC_KEY`, `VAPID_PRIVATE_KEY` (base64url, from
`GenerateVAPIDKeys`) and `VAPID_SUBJECT` (`mailto:…`) go in `infra/.env` and are passed by
`infra/compose.yml` to `backend-go` only, all defaulting to empty. Push is enabled only when all
three are set and `VAPID_SUBJECT` starts with `mailto:` or `https:`. If only some are set, the
backend logs one WARN at startup and treats push as disabled. Key generation is
`cd apps/main/backend-go && go run ./cmd/vapidkeys` (Go is installed on the Pi), which prints the
two `.env` lines. **Rotating the pair invalidates every subscription.** The push services then
answer `403`. On its next visit to Settings, each device sees that its subscription's
`applicationServerKey` differs from `GET /push/config` and re-subscribes by itself. A device that
never comes back keeps one stale row, which costs one `403` per send.

### 8. Where the code lives (task decision 7)

Everything goes in the main app: `apps/main/backend-go/internal/push`, `cmd/vapidkeys`, and the
frontend's `src/lib/push.ts`, `src/hooks/usePushSubscription.ts`, the Settings card and
`public/sw.js`. **Nothing goes in `packages/common` yet.** There is one consumer. The AGENTS.md
rule fires on real duplication. A second app on this same origin could not reuse a root `sw.js`
anyway: it would need its own scope (`/portfolio-b/`) and VAPID pair, and that design is unknown today.
Move the pure browser helpers to `packages/common/ts` when a second app needs them.

### 9. Placement of the UI: Settings

The card is **"Powiadomienia"** on `/settings`, the page that already holds per-installation
configuration. `/news` stays a pure reader. The card is hidden when `enabled` is false or the
browser lacks `serviceWorker`, `PushManager` or `Notification`. Its states are in the design doc
§5.

### 10. Testability (question 6)

- `push.Sender` is a one-method interface (`Send(ctx, Subscription, Notification) (status int,
  err error)`). `webpushSender` is the only production implementation.
- The service and handler integration tests run against Testcontainers Postgres with every
  migration applied. The DB is never mocked. They use a fake `Sender` that is scripted per
  endpoint.
- One adapter test points `webpushSender` at a local `httptest.Server` that stands in for the
  push service. It asserts the `aes128gcm` / `vapid` / `TTL` / `Urgency` headers, and that the
  service maps `201`, `404`, `410` and `429` to `delivered` / `pruned` (row gone) / `pruned` /
  `failed` (row kept).

## Pattern evaluation

| Candidate | Verdict | Why |
| --- | --- | --- |
| Adapter (`webpushSender` wraps `webpush-go` behind `push.Sender`) | Adopted | It isolates the third-party API and its `mailto:` and body-closing quirks, and gives tests a seam (DIP). |
| Facade (`push.Service`: subscribe, unsubscribe, `Broadcast`, `SendToUser`) | Adopted | It is the one entry point for every consumer, and it owns the send-result policy table (§4). |
| Consumer-side interface (`news.Notifier`, satisfied by `*push.Service`) | Adopted | The news package depends on an abstraction it defines. It imports only push's value types (`Notification`, `DeliveryReport`), never `push.Service`. The wiring happens in `cmd/server`, like `botPriceAdapter`. |
| Narrow `Queries` interface over `*sqlcgen.Queries` | Adopted | It follows the existing package convention (`news.Queries`). No hand-written repository struct. |
| Observer / event bus for "report produced" | Rejected | There is one producer and one subscriber type. A direct call is simpler (YAGNI). |
| Strategy for several channels (push, e-mail, ntfy) | Rejected | There is one channel. ntfy is removed, not kept as an alternative. |
| Retry queue or outbox for sends | Rejected | One message a day, best-effort. The push service already stores messages for offline devices up to the TTL. |
| Template Method or a templating engine for the copy | Rejected | A five-row `switch` in `news` is clearer. |

## Alternatives considered

- **Keep ntfy.** Rejected by the owner: it needs another phone app.
- **The backend pushes after `POST /news/ingest` with no new endpoint.** Rejected for ADR-107's
  reason: the backend never sees the empty or failed nights.
- **A free-text `{title, body}` notify endpoint.** Rejected: it lets any token holder push
  arbitrary text, and it moves the privacy rule out of code and into a shell script.
- **A self-hosted push relay or Gotify.** Rejected: Android Chrome only accepts its vendor's push
  service, so a relay would still need an app.

## Consequences

- ntfy is gone: `NTFY_*`, `NEWS_APP_URL`, the ntfy `notify()` publisher and the README section are
  removed. The owner deletes `NTFY_TOPIC` from the Pi's `infra/.env`, adds the three `VAPID_*`
  lines, and runs `up -d backend-go` (orchestrator).
- The external surface grows by one: the backend now makes outbound HTTPS to browser push services
  (`docs/architecture/01-system-context.md`). The script no longer calls any third party.
- One new table, one new package, one new frontend card, and two static files. The default without
  `VAPID_*` is off, and nothing else changes.
- HTTP 2xx from FCM proves delivery to the vendor, not to the screen. Android battery or
  notification settings can still hide a message, so the on-phone check with the test button is
  part of the definition of done.
- Living docs updated with this ADR: `01-system-context.md`, `06-data-flow.md` §6,
  `03-api-contract.md` (Push section), `04-db-schema.md` (`push_subscriptions`).
