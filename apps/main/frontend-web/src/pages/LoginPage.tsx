import { useState } from "react";
import { GoogleLogin } from "@react-oauth/google";
import { Navigate, useNavigate } from "react-router";
import { useAuthStore } from "@/lib/auth";
import { exchangeGoogleToken } from "@/lib/authApi";
import { NebulaCanvas } from "@/components/NebulaCanvas";

export default function LoginPage() {
  const navigate = useNavigate();
  const login = useAuthStore((s) => s.login);
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const hasHydrated = useAuthStore((s) => s.hasHydrated);
  const [error, setError] = useState<string | null>(null);

  // A home-screen PWA shortcut may open straight at /login even with a valid
  // persisted session — bounce to the app instead of asking for Google again.
  if (hasHydrated && isAuthenticated) {
    return <Navigate to="/" replace />;
  }

  async function handleSuccess(googleIdToken: string) {
    setError(null);
    try {
      const { token, expiresAt } = await exchangeGoogleToken(googleIdToken);
      login(token, expiresAt);
      void navigate("/", { replace: true });
    } catch (e) {
      console.error("Token exchange failed", e);
      setError("Logowanie nie powiodło się. Spróbuj ponownie.");
    }
  }

  return (
    <div className="relative flex min-h-screen items-center justify-center overflow-hidden bg-background">
      <NebulaCanvas />
      <div className="relative z-10 flex flex-col items-center gap-8 rounded-2xl px-10 py-12 card-psychedelic">
        <div className="flex items-center gap-3">
          <span className="brand-mark" aria-hidden="true" />
          <span className="font-display text-4xl font-bold tracking-tight gradient-text-tri select-none">
            Inwestycje
          </span>
        </div>
        <p className="-mt-6 text-sm text-muted-foreground">
          Zaloguj się, aby zobaczyć portfel
        </p>
        <LoginButton onSuccess={handleSuccess} />
        {error && (
          <p role="alert" className="text-sm text-destructive">{error}</p>
        )}
      </div>
    </div>
  );
}

function LoginButton({
  onSuccess,
}: {
  onSuccess: (credential: string) => Promise<void>;
}) {
  return (
    <GoogleLogin
      onSuccess={(credentialResponse) => {
        const idToken = credentialResponse.credential;
        if (idToken) {
          void onSuccess(idToken);
        }
      }}
      onError={() => {
        // Google's button UI surfaces the error — nothing to do here
      }}
      useOneTap
    />
  );
}
