import { useCallback, useState } from "react";
import { newIdempotencyKey } from "../client/idempotency.js";

/**
 * One idempotency key per user intent (ADR-111). Held by the component that owns a dialog or
 * form: pass `key` to the money-moving mutation, reuse it on retry, and `rotate()` once the
 * intent is over (success, or the dialog closing) so the next submission is a new operation.
 */
export function useIdempotencyKey(): { key: string; rotate: () => void } {
  const [key, setKey] = useState(newIdempotencyKey);
  const rotate = useCallback(() => setKey(newIdempotencyKey()), []);
  return { key, rotate };
}
