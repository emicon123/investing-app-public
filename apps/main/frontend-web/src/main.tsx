import { setUnauthenticatedHandler, setupPreloadErrorRecovery, bootstrapApp, configureApiClient } from "@common/app-core";
import App from "./App";
import { Toaster } from "@/components/ui/sonner";
import { useAuthStore } from "@/lib/auth";
import "./index.css";

// When the API returns 401 (token expired, revoked, or email removed from the
// allowlist), drop the stored session so ProtectedRoute bounces back to /login.
setUnauthenticatedHandler(() => {
  useAuthStore.getState().logout();
});

setupPreloadErrorRecovery();

configureApiClient({ baseURL: "/api" });

bootstrapApp({
  googleClientId: import.meta.env.VITE_GOOGLE_CLIENT_ID as string,
  children: (
    <>
      <App />
      <Toaster />
    </>
  ),
});
