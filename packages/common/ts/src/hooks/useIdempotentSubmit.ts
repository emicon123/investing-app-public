import { isIdempotencyInProgress, isIdempotencyKeyReused } from "../client/errors.js";
import type { IdempotencyOptions } from "../client/idempotency.js";
import { useIdempotencyKey } from "./useIdempotencyKey.js";

/**
 * Binds one idempotency key (per dialog/form open) to a money-moving client call (ADR-111).
 *
 * - A failed attempt keeps the key, so a retry of the same request replays instead of repeating.
 * - Success rotates it: the next submission is a new operation.
 * - `IDEMPOTENCY_IN_PROGRESS` (outcome of an earlier attempt unknown) rotates it too and rethrows
 *   the original error; render {@link IDEMPOTENCY_IN_PROGRESS_MESSAGE} for it.
 * - `IDEMPOTENCY_KEY_REUSED` (the form was edited after an unknown-outcome attempt) rotates it as
 *   well, so the edited form's next submit is a new operation; render
 *   {@link IDEMPOTENCY_KEY_REUSED_MESSAGE}. Use `idempotencyErrorMessage` to pick either message.
 * - `rotate` is returned for the owner to call when the dialog closes or the plan is discarded.
 */
export function useIdempotentSubmit<TArgs, TResult>(
  send: (args: TArgs, options: IdempotencyOptions) => Promise<TResult>,
): { submit: (args: TArgs) => Promise<TResult>; rotate: () => void } {
  const { key, rotate } = useIdempotencyKey();

  async function submit(args: TArgs): Promise<TResult> {
    try {
      const result = await send(args, { idempotencyKey: key });
      rotate();
      return result;
    } catch (err) {
      if (isIdempotencyInProgress(err) || isIdempotencyKeyReused(err)) rotate();
      throw err;
    }
  }

  return { submit, rotate };
}
