import axios, { type AxiosError } from "axios";
import { ApiError } from "./errors.js";
import { getToken, shouldLogoutOnUnauthorized, triggerUnauthenticated } from "../auth/token-store.js";

/**
 * Shared Axios singleton for all three frontends (task 158, D1). The three legacy
 * per-app `axios-instance.ts` copies were byte-identical except `baseURL`, which is a
 * load-bearing per-app Nginx path prefix (`/api` vs `/portfolio-b/api` vs `/portfolio-c/api`) —
 * so the URL lives with each app, not here. Each app's `main.tsx` calls
 * `configureApiClient({ baseURL })` once at startup (same once-per-app pattern as
 * `bootstrapApp`); the `"/api"` default below only covers the root app if that call
 * were ever skipped. Resource clients import `{ apiClient }` unchanged — only the
 * import path moved from `@*-app/shared` to `@common/app-core`.
 */
export const apiClient = axios.create({
  baseURL: "/api",
  headers: { "Content-Type": "application/json" },
  timeout: 60_000,
});

export function configureApiClient(options: { baseURL: string }): void {
  apiClient.defaults.baseURL = options.baseURL;
}

apiClient.interceptors.request.use((config) => {
  const token = getToken();
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

apiClient.interceptors.response.use(
  (response) => response,
  (err: AxiosError) => {
    // Only drop the session when a presented Bearer token was rejected — a
    // tokenless 401 (e.g. pre-hydration on a cold PWA launch) or a 401 from
    // the public /auth/token exchange must never wipe a valid stored session.
    if (shouldLogoutOnUnauthorized(err)) {
      triggerUnauthenticated();
    }
    return Promise.reject(ApiError.fromAxiosError(err));
  },
);
