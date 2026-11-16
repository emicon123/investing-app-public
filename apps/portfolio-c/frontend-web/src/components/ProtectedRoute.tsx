import { useAuthStore } from "@/lib/auth";
import { ProtectedRoute as SharedProtectedRoute } from "@common/ui-primitives/ProtectedRoute";

export default function ProtectedRoute() {
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const hasHydrated = useAuthStore((s) => s.hasHydrated);
  return <SharedProtectedRoute isAuthenticated={isAuthenticated} hasHydrated={hasHydrated} />;
}
