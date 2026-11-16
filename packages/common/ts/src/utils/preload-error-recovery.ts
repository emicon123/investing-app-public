/**
 * Vite content-hashes route chunks; a browser tab open across a redeploy can
 * request a chunk hash the new deploy no longer serves ("Failed to fetch
 * dynamically imported module"). Auto-reload once per tab session to recover
 * instead of leaving the user stuck on a broken route. Capped at one reload
 * per session so a genuinely broken deploy doesn't reload-loop the tab.
 */
export function setupPreloadErrorRecovery(): void {
  window.addEventListener("vite:preloadError", () => {
    if (sessionStorage.getItem("preload-error-reloaded")) return;
    sessionStorage.setItem("preload-error-reloaded", "1");
    window.location.reload();
  });
}
