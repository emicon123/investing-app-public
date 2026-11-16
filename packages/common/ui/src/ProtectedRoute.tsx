import { Navigate, Outlet } from "react-router";

// Gates the app behind a valid Google session. The auth store's `persist`
// middleware restores from localStorage asynchronously, so until hydration
// finishes (`hasHydrated`) we render nothing rather than redirect — deciding
// "unauthenticated" from the pre-hydration default would bounce users with a
// perfectly valid persisted session to /login (see ADR-081). Once hydrated,
// unauthenticated visitors are bounced to the login screen; the backend
// independently rejects any API call without a valid Bearer token, so this is
// UX, not the security boundary.
export function ProtectedRoute({
  isAuthenticated,
  hasHydrated,
}: {
  isAuthenticated: boolean;
  hasHydrated: boolean;
}) {
  if (!hasHydrated) {
    return null;
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" replace />;
  }

  return <Outlet />;
}
