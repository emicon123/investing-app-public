let _token: string | null = null;
let _tokenGetter: () => string | null = () => _token;
let _unauthenticatedHandler: () => void = () => undefined;

export function getToken(): string | null {
  return _tokenGetter();
}

export function setTokenGetter(fn: () => string | null): void {
  _tokenGetter = fn;
}

export function setToken(token: string): void {
  _token = token;
}

export function clearToken(): void {
  _token = null;
}

export function setUnauthenticatedHandler(fn: () => void): void {
  _unauthenticatedHandler = fn;
}

export function triggerUnauthenticated(): void {
  _unauthenticatedHandler();
}

/**
 * Decides whether a failed API call means "our stored session is dead, log out".
 *
 * Guards each app's global 401 → logout axios wiring. A bare
 * `status === 401` check is too eager: it also fires for requests that never
 * carried a token (e.g. fired before the persisted store hydrated on a cold
 * PWA launch) and for the public POST /auth/token exchange itself — wiping a
 * perfectly valid 30-day session and forcing a needless Google re-login
 * (PWA-only symptom: browser tabs are usually warm, PWA launches are cold).
 * Only a *presented* Bearer token that the backend *rejects* means logout.
 *
 * Structurally typed (no axios import) so every app's axios-instance can use
 * it regardless of axios copy.
 */
export function shouldLogoutOnUnauthorized(err: {
  response?: { status?: number };
  config?: { url?: string; headers?: unknown };
}): boolean {
  if (err.response?.status !== 401) return false;
  const url = err.config?.url ?? "";
  // The token-issuing endpoint is public by design — its 401 (bad Google
  // token) says nothing about our stored session.
  if (url.endsWith("/auth/token")) return false;
  return didSendBearerToken(err.config?.headers);
}

function didSendBearerToken(headers: unknown): boolean {
  if (!headers || typeof headers !== "object") return false;
  const withGet = headers as { get?: unknown };
  if (typeof withGet.get === "function") {
    return Boolean((withGet.get as (name: string) => unknown)("Authorization"));
  }
  return Boolean((headers as Record<string, unknown>)["Authorization"]);
}
