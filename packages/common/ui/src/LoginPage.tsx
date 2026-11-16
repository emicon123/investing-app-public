import { GoogleLogin } from "@react-oauth/google";
import { useNavigate } from "react-router";

/**
 * portfolioB/portfolioC only — investing-app's LoginPage got a real Prism Ledger visual
 * redesign and is not part of this extraction (see task 89).
 */
export interface LoginPageProps {
  title: string;
  login: (backendToken: string, expiresAt: string) => void;
  exchangeGoogleToken: (googleIdToken: string) => Promise<{ token: string; expiresAt: string }>;
}

export function LoginPage({ title, login, exchangeGoogleToken }: LoginPageProps) {
  const navigate = useNavigate();

  async function handleSuccess(googleIdToken: string) {
    try {
      const { token, expiresAt } = await exchangeGoogleToken(googleIdToken);
      login(token, expiresAt);
      void navigate("/", { replace: true });
    } catch {
      console.error("Token exchange failed");
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-background">
      <div className="flex flex-col items-center gap-8">
        <div className="text-center">
          <h1 className="text-3xl font-bold tracking-tight">{title}</h1>
          <p className="mt-2 text-sm text-muted-foreground">
            Sign in to access your portfolio
          </p>
        </div>
        <GoogleLogin
          onSuccess={(credentialResponse) => {
            const idToken = credentialResponse.credential;
            if (idToken) void handleSuccess(idToken);
          }}
          onError={() => undefined}
          useOneTap
        />
      </div>
    </div>
  );
}
