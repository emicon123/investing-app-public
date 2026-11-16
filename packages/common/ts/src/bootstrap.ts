import { createElement, StrictMode, type ReactNode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter } from "react-router";
import { GoogleOAuthProvider } from "@react-oauth/google";

/**
 * Plain `.ts` file (no JSX syntax) so it stays valid under this package's
 * `tsconfig.json`, which does not set a `jsx` compiler option — the tree is
 * built with `createElement` instead.
 */
export interface BootstrapAppOptions {
  /** `import.meta.env.VITE_GOOGLE_CLIENT_ID` from the calling app. */
  googleClientId: string;
  /** Router basename, e.g. "/portfolio-b" or "/portfolio-c". Omit when the app is served at `/`. */
  basename?: string;
  /** `<App />` plus any siblings (e.g. `<Toaster />`) to mount inside the providers. */
  children: ReactNode;
  /** Defaults to a fresh `QueryClient()` if omitted. */
  queryClient?: QueryClient;
  /** DOM element id to mount into. Defaults to "root". */
  rootElementId?: string;
}

/**
 * Shared app-shell bootstrap: mounts `children` inside
 * `StrictMode > GoogleOAuthProvider > BrowserRouter > QueryClientProvider`.
 * Each app's `main.tsx` shrinks to its own `setUnauthenticatedHandler` /
 * `setupPreloadErrorRecovery` wiring plus one call to this function with its
 * own `basename` and `googleClientId`.
 *
 * Per-app theme selection (`data-app-theme="portfolioB"|"portfolioC"`, task 125 §3
 * step 3) is *not* wired here — it's set statically on `<html>` in each
 * app's `index.html` (alongside the existing static `class="dark"`) rather
 * than by JS at boot, so the themed background paints on first frame with
 * no flash-of-unthemed-content window before this function runs.
 */
export function bootstrapApp(options: BootstrapAppOptions): void {
  const { googleClientId, basename, children, queryClient, rootElementId = "root" } = options;
  const client = queryClient ?? new QueryClient();

  const rootElement = document.getElementById(rootElementId);
  if (!rootElement) throw new Error("Root element not found");

  // `createElement`'s TS overloads don't merge trailing ...children args into
  // a component's *required* `children` prop (e.g. GoogleOAuthProviderProps),
  // so each nesting level passes `children` explicitly on the props object
  // instead of as a rest argument.
  const queryProviderElement = createElement(QueryClientProvider, { client, children });
  const routerElement = createElement(BrowserRouter, {
    ...(basename ? { basename } : null),
    children: queryProviderElement,
  });
  const oauthElement = createElement(GoogleOAuthProvider, {
    clientId: googleClientId,
    children: routerElement,
  });
  const tree = createElement(StrictMode, { children: oauthElement });

  createRoot(rootElement).render(tree);
}
