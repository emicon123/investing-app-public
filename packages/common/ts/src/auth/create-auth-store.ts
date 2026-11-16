import { create, type StoreApi } from "zustand";
import { persist } from "zustand/middleware";
import { setToken, clearToken } from "./token-store.js";

export interface AuthUser {
  email: string;
  name: string;
  picture: string;
  exp: number;
}

interface AuthState {
  isAuthenticated: boolean;
  hasHydrated: boolean;
  user: AuthUser | null;
  token: string | null;
  expiresAt: string | null;
  login: (backendToken: string, expiresAt: string) => void;
  logout: () => void;
}

function parseJwtPayload(token: string): Record<string, unknown> {
  const parts = token.split(".");
  if (parts.length !== 3) throw new Error("Invalid JWT format");
  const base64 = parts[1]!.replace(/-/g, "+").replace(/_/g, "/");
  const padded = base64.padEnd(base64.length + ((4 - (base64.length % 4)) % 4), "=");
  return JSON.parse(atob(padded)) as Record<string, unknown>;
}

export function extractUserFromBackendJwt(token: string, expiresAt: string): AuthUser {
  const payload = parseJwtPayload(token);
  return {
    email: typeof payload.email === "string" ? payload.email : (typeof payload.sub === "string" ? payload.sub : ""),
    name: typeof payload.name === "string" ? payload.name : "",
    picture: typeof payload.picture === "string" ? payload.picture : "",
    exp: new Date(expiresAt).getTime() / 1000,
  };
}

/**
 * Shared by all three apps (investing-app, portfolioB, portfolioC) as of commit 93fe569.
 */
export function createAuthStore(storageKey: string) {
  // Captured from the state-creator closure below so onRehydrateStorage — a
  // sibling option, not nested inside that closure — can also call `set`.
  let setState!: StoreApi<AuthState>["setState"];

  return create<AuthState>()(
    persist(
      (set) => {
        setState = set;
        return {
          isAuthenticated: false,
          hasHydrated: false,
          user: null,
          token: null,
          expiresAt: null,
          login(backendToken: string, expiresAt: string) {
            const user = extractUserFromBackendJwt(backendToken, expiresAt);
            setToken(backendToken);
            set({ isAuthenticated: true, user, token: backendToken, expiresAt });
          },
          logout() {
            clearToken();
            set({ isAuthenticated: false, user: null, token: null, expiresAt: null });
          },
        };
      },
      {
        name: storageKey,
        onRehydrateStorage: () => (state) => {
          // Runs exactly once after hydration completes, whether or not
          // localStorage held anything — mark hydration done regardless.
          setState({ hasHydrated: true });
          if (!state) return;
          const { token, expiresAt, logout } = state;
          if (token && expiresAt) {
            if (new Date(expiresAt) <= new Date()) {
              // Token expired — clear persisted state
              logout();
            } else {
              // Restore the axios interceptor token
              setToken(token);
            }
          }
        },
      },
    ),
  );
}
