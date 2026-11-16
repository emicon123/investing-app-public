import { Navigate } from "react-router";
import { LoginPage as SharedLoginPage } from "@common/ui-primitives/LoginPage";
import { useAuthStore } from "@/lib/auth";
import { exchangeGoogleToken } from "@/lib/authApi";

export default function LoginPage() {
  const login = useAuthStore((s) => s.login);
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const hasHydrated = useAuthStore((s) => s.hasHydrated);

  // A home-screen PWA shortcut may open straight at /login even with a valid
  // persisted session — bounce to the app instead of asking for Google again.
  if (hasHydrated && isAuthenticated) {
    return <Navigate to="/" replace />;
  }

  return (
    <SharedLoginPage
      title="PortfolioB Investments"
      login={login}
      exchangeGoogleToken={exchangeGoogleToken}
    />
  );
}
