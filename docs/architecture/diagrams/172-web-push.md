# Task 172: Web Push Notifications (design and diagrams)

> **Decision record:** `docs/adr/ADR-108-web-push-notifications.md`. **Contract:**
> `docs/openapi.yaml` (operations `getPushConfig`, `upsertPushSubscription`, `deletePushSubscription`,
> `sendTestPushNotification`, `notifyNewsReport`). **Schema:** `docs/architecture/04-db-schema.md`
> § Table `push_subscriptions` (Flyway `V204`). This file shows the system as it will be built.
> Each diagram has its own legend.

## 1. Pattern evaluation (summary)

| Candidate | Verdict | Where |
| --- | --- | --- |
| Adapter | adopted | `push.webpushSender` adapts `webpush-go` to `push.Sender` |
| Facade | adopted | `push.Service`: the one entry point for every consumer; owns the send-result policy |
| Dependency Inversion (consumer-side interface) | adopted | `news.Notifier` (one method) is satisfied by `*push.Service`, wired in `cmd/server` |
| Narrow `Queries` interface over sqlc | adopted | same convention as `news.Queries`; no hand-written repository |
| Observer/event bus, Strategy over channels, retry queue/outbox, Template Method for the copy | rejected | YAGNI: one producer, one channel, one message a day, best-effort (ADR-108, Pattern evaluation) |

Principles: SRP (the sender knows HTTP and crypto, the service knows the policy, news knows the
copy). OCP (the next consumer calls `Broadcast` and nothing is modified). KISS (sequential
fan-out, no goroutines, no queue).

## 2. C4: system context (what changes)

```mermaid
flowchart LR
    Owner(["Owner (Android Chrome,<br/>optionally the installed app)"])

    subgraph Pi["Raspberry Pi"]
        subgraph Docker["Docker Compose stack"]
            Nginx["nginx<br/>serves /sw.js, /manifest.webmanifest<br/>proxies /api/push/*, /api/news/*"]
            Backend["backend-go<br/>internal/push (new)<br/>internal/news (+notify)"]
            DB[("PostgreSQL<br/>push_subscriptions (V204)")]
        end
        Script["infra/news/news-research.sh<br/>host cron 06:15"]
    end

    PushSvc["Browser push service<br/>FCM / Mozilla autopush / Apple / WNS<br/>(external, free, run by the browser vendor)"]
    Ntfy["ntfy.sh<br/>(REMOVED)"]

    Owner -- "HTTPS (Tailscale Funnel):<br/>SPA, /api/push/* (bearer JWT)" --> Nginx
    Nginx --> Backend
    Backend --> DB
    Script -- "POST /api/news/notifications<br/>(X-Service-Token)" --> Nginx
    Backend -- "HTTPS POST endpoint<br/>VAPID + aes128gcm (RFC 8291/8292)" --> PushSvc
    PushSvc -- "push message<br/>(vendor channel)" --> Owner
    Script -. "no longer called" .-> Ntfy

    classDef removed stroke-dasharray: 5 5,color:#888;
    class Ntfy removed;
```

**Legend.** Solid edges are new or unchanged runtime calls. The dashed node and edge are removed
by this task. Boundary (policy vs detail): the copy and the send-result policy live in
`backend-go` (policy). The push service, the browser and nginx are details. The payload is
end-to-end encrypted to the browser, so the push service sees only ciphertext and the endpoint.

## 3. Subscribe flow (Settings, "Włącz na tym urządzeniu")

```mermaid
sequenceDiagram
    actor Owner
    participant UI as Settings page<br/>NotificationsSection
    participant Lib as src/lib/push.ts
    participant SW as /sw.js (service worker)
    participant API as backend-go<br/>push.Handler / push.Service
    participant DB as PostgreSQL
    participant PS as Push service (FCM)

    UI->>API: GET /api/push/config (bearer)
    API-->>UI: 200 {enabled, vapidPublicKey}
    alt enabled false, or no serviceWorker / PushManager / Notification
        UI->>UI: hide the card
    end
    Owner->>UI: tap "Włącz na tym urządzeniu"
    UI->>Lib: enable(vapidPublicKey)
    Lib->>Lib: Notification.requestPermission()
    alt permission denied
        Lib-->>UI: state DENIED (show the "zablokowane" hint)
    else granted
        Lib->>SW: navigator.serviceWorker.register("/sw.js", {scope: "/"})
        Lib->>PS: registration.pushManager.subscribe({userVisibleOnly: true, applicationServerKey})
        PS-->>Lib: PushSubscription {endpoint, keys.p256dh, keys.auth}
        Lib->>API: POST /api/push/subscriptions {endpoint, keys} (bearer)
        API->>API: validate https + host allowlist (else 422)<br/>owner = JWT principal, label = DeviceLabel(User-Agent)
        API->>DB: INSERT ... ON CONFLICT (endpoint) DO UPDATE
        API-->>Lib: 200 {id, deviceLabel, createdAt, updatedAt}
        Lib-->>UI: state ON
    end

    Note over UI,API: Every later Settings visit while ON: if the subscription's<br/>applicationServerKey differs from vapidPublicKey (keys rotated),<br/>unsubscribe and subscribe again. Then re-POST (an idempotent<br/>upsert that restores a pruned row).

    Owner->>UI: tap "Wyłącz na tym urządzeniu"
    UI->>Lib: disable()
    Lib->>PS: subscription.unsubscribe()
    Lib->>API: DELETE /api/push/subscriptions {endpoint} (bearer)
    API->>DB: DELETE WHERE endpoint = $1
    API-->>Lib: 204
```

**Legend.** The browser half is a thin Facade (`src/lib/push.ts`) under a React hook, so the card
holds no Push API calls. The endpoint goes only in request bodies, never in a URL (ADR-108 §7).
The upsert makes subscribe idempotent, so no "already subscribed" error path is needed (KISS).

## 4. Morning notify flow (06:15 cron, all four outcomes)

```mermaid
sequenceDiagram
    participant Cron as Host crontab 06:15
    participant Script as news-research.sh
    participant API as backend-go<br/>news.Handler / news.Service
    participant Push as push.Service
    participant Sender as webpushSender
    participant DB as PostgreSQL
    participant PS as Push service
    participant SW as Phone: /sw.js

    Cron->>Script: run
    Script->>Script: assets, claude, ingest (unchanged, 06-data-flow §6)
    Script->>Script: outcome = READY | NO_NEWS | NO_ASSETS | FAILED<br/>(FAILED also from the EXIT trap, exactly once per run)
    Script->>API: POST /api/news/notifications {outcome, newsDate, itemCount?}<br/>X-Service-Token, curl --max-time 30 --retry 2 --retry-connrefused
    API->>API: validate (422 on a bad outcome/itemCount combination)
    API->>API: ReportNotification(outcome, date, n), giving title, body,<br/>urgency, url "/news", tag "news-report", TTL 12h
    API->>Push: Broadcast(ctx, notification)
    alt push not configured
        Push-->>API: ErrNotConfigured
        API-->>Script: 503 PUSH_NOT_CONFIGURED
    else configured
        Push->>DB: ListPushSubscriptions()
        loop each subscription, sequentially, 5 s timeout each
            Push->>Sender: Send(ctx, sub, notification)
            Sender->>PS: POST endpoint, encrypted payload<br/>Authorization: vapid t=..., k=... / TTL / Urgency / Topic
            PS-->>Sender: status
            Sender-->>Push: status, err
            alt 2xx
                Push->>DB: MarkPushSubscriptionSuccess(id)
            else 404 / 410
                Push->>DB: DeletePushSubscriptionByID(id)
            else 401 / 403 / 429 / 5xx / other / network error
                Push->>Push: log id, host and status. Keep the row.
            end
        end
        Push-->>API: DeliveryReport {attempted, delivered, pruned, failed}
        API-->>Script: 200 {data: report}
        PS-)SW: push event (when the device is reachable, within the TTL)
        SW->>SW: showNotification(title, {body, tag, renotify, icon, badge, data: {url}})
    end
    Script->>Script: log the HTTP status and counts. The exit code is unchanged.
```

**Legend.** The Facade (`push.Service`) owns the send-result policy table (ADR-108 §4). The
Adapter (`webpushSender`) only encrypts, signs and posts. The news package maps outcome to copy
and depends on the push service only through `news.Notifier` (DIP). If the backend is down, the
script's call fails after its retries and the night stays silent. That gap is accepted
(ADR-108 §3).

## 5. Test-button flow and the card's states

```mermaid
sequenceDiagram
    actor Owner
    participant UI as NotificationsSection
    participant API as backend-go push.Handler
    participant Push as push.Service
    participant PS as Push service
    participant SW as /sw.js
    participant Tab as App tab /news

    Owner->>UI: tap "Wyślij testowe powiadomienie"
    UI->>API: POST /api/push/test-notifications (bearer)
    API->>Push: SendToUser(ctx, principalEmail, TestNotification)
    Push->>PS: one send per subscription of THIS user (same policy as Broadcast)
    Push-->>API: DeliveryReport
    API-->>UI: 200 {attempted, delivered, pruned, failed}
    UI->>UI: toast "Wysłano: delivered/attempted"<br/>(or "Brak zarejestrowanych urządzeń" when attempted = 0)
    PS-)SW: push
    SW->>SW: showNotification("Powiadomienie testowe", tag "push-test")
    Owner->>SW: tap the notification
    SW->>Tab: focus a client already on /news, else clients.openWindow("/news")
```

```mermaid
stateDiagram-v2
    [*] --> Loading
    Loading --> Hidden: config.enabled false, or Push API unsupported
    Loading --> Denied: Notification.permission is "denied"
    Loading --> Off: no subscription on this browser
    Loading --> On: subscription exists (resubscribe if the key differs, re-POST)
    Off --> Busy: tap Włącz
    Busy --> On: permission granted, subscribe + POST ok
    Busy --> Denied: permission refused
    Busy --> Off: error (toast)
    On --> Busy: tap Wyłącz
    Busy --> Off: unsubscribe + DELETE ok
    On --> On: tap "Wyślij testowe powiadomienie" (toast with counts)
    Hidden --> [*]
```

**Legend.** These are UI states of one browser (State, modelled as a discriminated union in the
hook, not as GoF State classes: there are five states and no per-state behaviour objects).
`Denied` shows the text "Powiadomienia są zablokowane w ustawieniach przeglądarki dla tej
strony." The `On` and `Off` states show the iOS hint "Na iPhonie działa tylko po dodaniu aplikacji
do ekranu głównego."

## 6. ERD

```mermaid
erDiagram
    push_subscriptions {
        BIGSERIAL id PK
        VARCHAR_320 user_email "JWT principal, NOT NULL"
        TEXT endpoint UK "https only, max 2048, capability: never logged"
        VARCHAR_128 p256dh "base64url, NOT NULL"
        VARCHAR_64 auth "base64url, NOT NULL"
        VARCHAR_100 device_label "from User-Agent, NOT NULL"
        TIMESTAMPTZ created_at "NOT NULL DEFAULT now()"
        TIMESTAMPTZ updated_at "NOT NULL, set_updated_at() trigger"
        TIMESTAMPTZ last_success_at "NULL until the first 2xx"
    }
```

**Legend.** The table stands alone, with no foreign key: there is no users table (the allowlisted
e-mail is the identity, as everywhere in the app). `idx_push_subscriptions_user_email` serves
`SendToUser`. A row's lifecycle: upsert on subscribe, delete on unsubscribe or on a `404`/`410`.
There is no time-based retention. DTO boundary: `p256dh`, `auth`, `endpoint` and `user_email`
never appear in a response DTO (`PushSubscriptionInfo` carries only id, label and timestamps).

## 7. Class diagram (Go backend)

```mermaid
classDiagram
    direction LR
    namespace push {
        class Notification {
            <<value object>>
            +Title string
            +Body string
            +URL string
            +Tag string
            +Topic string
            +Urgency Urgency
            +TTL time.Duration
        }
        class Subscription {
            <<entity>>
            +ID int64
            +UserEmail string
            +Endpoint string
            +P256dh string
            +Auth string
            +DeviceLabel string
            +CreatedAt time.Time
            +UpdatedAt time.Time
            +LastSuccessAt *time.Time
        }
        class DeliveryReport {
            <<value object>>
            +Attempted int
            +Delivered int
            +Pruned int
            +Failed int
        }
        class Sender {
            <<interface>>
            +Send(ctx, Subscription, Notification) (status int, err error)
        }
        class Queries {
            <<interface>>
            +UpsertPushSubscription(ctx, params) (row, error)
            +DeletePushSubscriptionByEndpoint(ctx, endpoint) error
            +DeletePushSubscriptionByID(ctx, id) error
            +ListPushSubscriptions(ctx) ([]row, error)
            +ListPushSubscriptionsByUser(ctx, email) ([]row, error)
            +MarkPushSubscriptionSuccess(ctx, id) error
        }
        class Service {
            <<Facade>>
            +Config() (enabled bool, publicKey string)
            +Subscribe(ctx, SubscribeInput) (Subscription, error)
            +Unsubscribe(ctx, endpoint) error
            +Broadcast(ctx, Notification) (DeliveryReport, error)
            +SendToUser(ctx, email, Notification) (DeliveryReport, error)
        }
        class webpushSender {
            <<Adapter>>
            -publicKey string
            -privateKey string
            -subscriber string
            -client *http.Client
        }
        class Handler {
            +GetPushConfig(w, r)
            +UpsertPushSubscription(w, r)
            +DeletePushSubscription(w, r)
            +SendTestPushNotification(w, r)
        }
    }
    namespace news {
        class Notifier {
            <<interface>>
            +Broadcast(ctx, push.Notification) (push.DeliveryReport, error)
        }
        class NewsService["news.Service"] {
            +NotifyReport(ctx, ReportOutcome) (push.DeliveryReport, error)
        }
        class NewsHandler["news.Handler"] {
            +NotifyNewsReport(w, r)
        }
    }
    class webpushgo["webpush-go v1.4.0 (MIT)"] {
        <<external>>
        +SendNotificationWithContext()
        +GenerateVAPIDKeys()
    }

    Service --> Queries : uses
    Service --> Sender : uses
    Service ..> Notification
    Service ..> DeliveryReport
    Service ..> Subscription
    webpushSender ..|> Sender
    webpushSender --> webpushgo : adapts
    Handler --> Service
    NewsHandler --> NewsService
    NewsService --> Notifier : uses
    Service ..|> Notifier : satisfies, wired in cmd/server
```

**Legend.** `<<Facade>>`: `push.Service` is the single entry point and owns the policy (allowlist,
the `2xx`/`404`/`410`/other mapping, `ErrNotConfigured` when disabled). `<<Adapter>>`:
`webpushSender` is the only file that imports `webpush-go`. It strips `mailto:` from the subject
and closes the response body. DIP: `news` declares the one-method `Notifier`. It imports only
push's value types (`Notification`, `DeliveryReport`), and `cmd/server` passes `*push.Service`.
`Queries` is satisfied by `*sqlcgen.Queries`, and the tests use the real DB. `Sender` is the only
interface faked in tests.

### Clean-architecture audit (skill 16)

- **SRP:** the copy lives in `news` (`ReportNotification`, a pure function), the policy in
  `push.Service`, and the wire protocol in `webpushSender`. Nothing does two of these.
- **No speculative abstraction:** the only interfaces are `Sender` (a test seam, and a real
  third-party boundary), `Queries` (the house convention) and `Notifier` (it prevents a
  news-to-push concrete dependency). There is no channel registry, no queue and no generic
  "notification type" enum.
- **Law of Demeter:** handlers talk only to their own service. The service never hands the
  sqlc row out. Mapping goes row to `Subscription` to `PushSubscriptionInfo` DTO.
- **DRY:** `Broadcast` and `SendToUser` share one private `sendAll(ctx, []Subscription,
  Notification)`.

## 8. Go signatures (contract for Backend-Go)

```go
package push // apps/main/backend-go/internal/push

type Urgency string
const (UrgencyLow Urgency = "low"; UrgencyNormal Urgency = "normal"; UrgencyHigh Urgency = "high")

type Notification struct {
    Title, Body, URL, Tag, Topic string // URL is a relative path ("/news"). Topic is optional.
    Urgency Urgency
    TTL     time.Duration
}

// TestNotification is what POST /push/test-notifications sends (ADR-108 §5).
var TestNotification = Notification{Title: "Powiadomienie testowe",
    Body: "Powiadomienia działają na tym urządzeniu.", URL: "/news", Tag: "push-test",
    Urgency: UrgencyNormal, TTL: 5 * time.Minute}

type SubscribeInput struct{ UserEmail, Endpoint, P256dh, Auth, UserAgent string }
type DeliveryReport struct{ Attempted, Delivered, Pruned, Failed int }

type Sender interface {
    // Send encrypts and posts one message. status is the push service's HTTP status
    // (0 with a non-nil err on a network error or timeout). It never logs the endpoint.
    Send(ctx context.Context, sub Subscription, n Notification) (status int, err error)
}

type Config struct{ PublicKey, PrivateKey, Subject string }
func LoadConfig() Config            // VAPID_PUBLIC_KEY / VAPID_PRIVATE_KEY / VAPID_SUBJECT
func (c Config) Enabled() bool      // all three set and Subject has the prefix mailto: or https:

var ErrNotConfigured = errors.New("push: not configured")
var DefaultAllowedHosts = []string{"fcm.googleapis.com", "push.services.mozilla.com",
    "push.apple.com", "notify.windows.com"} // the host equals one of these or ends in "."+entry

func NewService(q Queries, s Sender, cfg Config, allowedHosts []string) *Service
func NewWebPushSender(cfg Config, client *http.Client) Sender // client.Timeout = 5 * time.Second
func DeviceLabel(userAgent string) string                    // "Android · Chrome", "iOS · Safari", …; at most 100 chars; "Nieznane urządzenie" when empty
```

```go
package news // additions

type Notifier interface {
    Broadcast(ctx context.Context, n push.Notification) (push.DeliveryReport, error)
}
type Outcome string // READY | NO_NEWS | NO_ASSETS | FAILED
type ReportOutcome struct { Outcome Outcome; NewsDate time.Time; ItemCount *int }

// ReportNotification is pure: it validates the outcome/itemCount pair and returns the ADR-108 §5
// copy (title, body, urgency, URL "/news", Tag and Topic "news-report", TTL 12h).
func ReportNotification(o ReportOutcome) (push.Notification, error)
func (s *Service) NotifyReport(ctx context.Context, o ReportOutcome) (push.DeliveryReport, error)
// NewService gains a third parameter: NewService(queries Queries, historyLookbackDays int, notifier Notifier)
```

Error mapping in the handlers: a validation error gives `422 VALIDATION_ERROR` (with `field`),
`push.ErrNotConfigured` gives `503 PUSH_NOT_CONFIGURED`, and anything else gives `500`, through
`common-go/httpx`.

## 9. Service-worker contract (`apps/main/frontend-web/public/sw.js`)

| Listener | Behaviour |
| --- | --- |
| `install` | `self.skipWaiting()` |
| `push` | `event.waitUntil(self.registration.showNotification(p.title, {body: p.body, tag: p.tag, renotify: true, icon: "/icons/icon-192.png", badge: "/icons/badge-96.png", data: {url: p.url}}))`, where `p = event.data.json()`. If the JSON is missing or invalid, show `Inwestycje` / `Nowe powiadomienie` with `url` `/news` (Chrome requires a visible notification for every push). |
| `notificationclick` | `close()`. If `data.url` is not a string starting with `/` (or starts with `//`), use `/news`. Focus a window client whose pathname equals it, else `clients.openWindow(url)`. |
| `fetch` | **must not exist.** The root scope also covers `/portfolio-b/` and `/portfolio-c/`. |

The payload is JSON: `{title, body, url, tag}`, all strings, produced only by the backend.
