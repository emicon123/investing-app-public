import { z } from "zod";

// ---------------------------------------------------------------------------
// POST /api/auth/token — request
// ---------------------------------------------------------------------------

/**
 * Request body for the token exchange endpoint.
 * The client sends the short-lived Google ID token obtained from Google
 * Sign-In. The backend validates it once and issues a long-lived backend JWT.
 */
export const TokenExchangeRequestSchema = z.object({
  googleIdToken: z
    .string()
    .min(1, "googleIdToken must not be blank")
    .describe(
      "Short-lived Google ID token obtained from Google Sign-In. " +
        "The backend validates signature, aud, and email claims."
    ),
});

export type TokenExchangeRequest = z.infer<typeof TokenExchangeRequestSchema>;

// ---------------------------------------------------------------------------
// POST /api/auth/token — response
// ---------------------------------------------------------------------------

/**
 * Response from the token exchange endpoint.
 * Store `token` in memory and send it as `Authorization: Bearer <token>` on every
 * subsequent API request.
 */
export const TokenExchangeResponseSchema = z.object({
  token: z
    .string()
    .min(1)
    .describe(
      "Backend-issued JWT signed with HMAC-SHA256. " +
        "Valid for 30 days by default (app.security.jwt-expiry-days)."
    ),
  expiresAt: z
    .string()
    .datetime({ offset: true })
    .describe("ISO 8601 UTC timestamp when the backend JWT expires."),
});

export type TokenExchangeResponse = z.infer<typeof TokenExchangeResponseSchema>;
