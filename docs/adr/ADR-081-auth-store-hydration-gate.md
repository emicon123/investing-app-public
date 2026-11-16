# ADR-081: Gate `ProtectedRoute` on Auth-Store Hydration (Android PWA Re-login Bug)

**Date:** 2026-09-01
**Status:** Accepted

## Context

Reported symptom: on the Android home-screen PWA install, the user is bounced to `/login`
and must re-authenticate via Google Sign-In almost every launch, even though the backend
JWT is valid for 30 days (`SecurityProperties.jwtExpiryDays = 30`, unchanged across all
three apps). This is not a token-expiry bug — the persisted token is still valid when the
redirect happens.

Root cause is a client-side hydration race in the shared auth stack
(`packages/shared/ts/src/auth/create-auth-store.ts`, `packages/shared/ui/src/ProtectedRoute.tsx`,
consumed identically by root `frontend-web`, `apps/portfolio-b/frontend-web`, and
`apps/portfolio-c/frontend-web`, each via a thin local `ProtectedRoute.tsx` wrapper):

1. `createAuthStore` builds a Zustand store wrapped in the `persist` middleware, which
   restores `token` / `expiresAt` / `user` / `isAuthenticated` from `localStorage`
   **asynchronously** — the store's in-memory default (`isAuthenticated: false`) is what
   the first synchronous render sees, regardless of what's in storage.
2. `ProtectedRoute` (shared) renders `<Navigate to="/login" replace />` the instant it sees
   `isAuthenticated === false`, with no way to distinguish "actually logged out" from
   "hydration hasn't finished yet."
3. `LoginPage.tsx` (per-app) has no effect watching for `isAuthenticated` flipping `true`
   after a delayed hydration, so once the redirect fires the user lands on a real login
   screen and must complete a fresh Google Sign-In — discarding a perfectly valid persisted
   session.

The race is intermittent on a warm browser tab: hydration from `localStorage` is normally
fast enough to win against the first render, or the tab may already be hydrated from a
previous visit in the same process. It bites **reliably** on an Android home-screen PWA
launch because every launch is a cold JS process start with no warm state to fall back on
— the race is lost almost every time. This matches the reported symptom exactly, and
explains why it wasn't caught on desktop browser testing (tabs are rarely fully cold there).

### Pattern evaluation

No new structural pattern is introduced. This is a **state-machine correction**, not a
design pattern gap: `isAuthenticated` is currently modeled as a 2-state boolean
(authenticated / not) when the real lifecycle has three states (unknown-pending /
authenticated / unauthenticated), and the redirect logic collapses the first two
incorrectly. The fix adds the missing third state (`hasHydrated`) rather than adopting a
new pattern — YAGNI applies against anything heavier (e.g. a full `stateDiagram-v2`-modeled
auth state machine) for what is fundamentally one boolean flag threaded through an existing
component tree.

**No diagram:** no new entities, services, or cross-component relationships are introduced
— `ProtectedRoute` gains one additional boolean prop, sourced from one additional store
field. The existing prose above and in `docs/architecture/12-portfolio-bpps-and-shared-packages.md`
§2.1/§5 fully describes the affected files; a diagram would not add information.

## Decision

Add a `hasHydrated: boolean` field to the store `createAuthStore` builds, defaulting to
`false` and flipped to `true` inside the `onRehydrateStorage` returned callback — which
Zustand's `persist` middleware guarantees runs exactly once after hydration completes,
whether or not `localStorage` held anything and whether or not hydration errored.

`ProtectedRoute` (shared) gains a `hasHydrated` prop. While `!hasHydrated` it renders
nothing (a blank frame — hydration from `localStorage` is near-instant, well under a
perceptible flash) instead of deciding anything about the redirect. Only once hydrated does
it evaluate `isAuthenticated` and either redirect to `/login` or render `<Outlet/>`.

Each app's local `ProtectedRoute.tsx` wrapper (root `frontend-web`, portfolio-b, portfolio-c) passes
`hasHydrated={useAuthStore((s) => s.hasHydrated)}` through alongside the existing
`isAuthenticated` prop — the same thin-wrapper pattern already used for `isAuthenticated`
(ADR-076/078, `packages/shared/ui` §2.1), so no new prop-threading pattern is introduced.

This is purely a client-side fix. No API contract, DB schema, or wire-format change is
involved — `jwtExpiryDays` and the backend auth flow (`AuthTokenFacade`,
`GoogleJwtAuthenticationConverter`) are unaffected and already correct.

Implementation is a follow-up frontend-web task; this ADR records the decision, not the
code.

## Consequences

- Fixes the Android PWA re-login bug for all three apps at once, since all three consume
  the same shared `createAuthStore` / `ProtectedRoute`.
- Trades a near-instant blank frame (pre-hydration) for correctness — acceptable given
  `localStorage` reads are effectively synchronous in wall-clock terms; no loading spinner
  is warranted for a gap this short.
- `LoginPage.tsx` does not need a new "watch for late hydration" effect — gating in
  `ProtectedRoute` before the redirect ever fires makes that workaround unnecessary and
  keeps the fix in one place instead of three (one per app's `LoginPage.tsx`).
- Any future consumer of `createAuthStore`/`ProtectedRoute` inherits the correct 3-state
  behavior automatically — no per-app opt-in required.
