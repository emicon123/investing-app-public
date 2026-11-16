/** Request header carrying the per-intent key of a money-moving POST (ADR-111). */
export const IDEMPOTENCY_HEADER = "Idempotency-Key";

/** Per-call options of the money-moving POST client functions. */
export interface IdempotencyOptions {
  idempotencyKey?: string;
}

const UUID_BYTES = 16;

/**
 * A fresh random v4 UUID. `crypto.randomUUID` exists only in secure contexts, so plain-HTTP LAN
 * access takes the `getRandomValues` path (available everywhere) and builds the same RFC 4122 v4.
 */
export function newIdempotencyKey(): string {
  if (typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  const bytes = crypto.getRandomValues(new Uint8Array(UUID_BYTES));
  bytes[6] = ((bytes[6] ?? 0) & 0x0f) | 0x40; // version 4
  bytes[8] = ((bytes[8] ?? 0) & 0x3f) | 0x80; // RFC 4122 variant
  const hex = Array.from(bytes, (b) => b.toString(16).padStart(2, "0"));
  return [
    hex.slice(0, 4).join(""),
    hex.slice(4, 6).join(""),
    hex.slice(6, 8).join(""),
    hex.slice(8, 10).join(""),
    hex.slice(10).join(""),
  ].join("-");
}

/** Axios `headers` for a request: `{}` without a key, so the header is simply omitted. */
export function idempotencyHeaders(key?: string): Record<string, string> {
  return key === undefined ? {} : { [IDEMPOTENCY_HEADER]: key };
}
