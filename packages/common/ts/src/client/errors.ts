import type { AxiosError } from "axios";

interface ErrorDetail {
  code: string;
  field?: string | null;
  message: string;
}

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    public readonly errors: ErrorDetail[],
    public readonly requestId: string,
  ) {
    super(errors[0]?.message ?? "API error");
    this.name = "ApiError";
  }

  static fromAxiosError(err: AxiosError): ApiError {
    const data = err.response?.data as Record<string, unknown> | undefined;
    const errors = (data?.errors as ErrorDetail[] | undefined) ?? [
      { code: "UNKNOWN", message: err.message },
    ];
    const meta = data?.meta as { requestId?: string } | undefined;
    return new ApiError(
      err.response?.status ?? 0,
      errors,
      meta?.requestId ?? "",
    );
  }
}

/**
 * The server still holds the key's first request (running, or ended with a 5xx whose outcome is
 * unknown): the caller must tell the user to check the history, then rotate the key (ADR-111).
 */
export function isIdempotencyInProgress(err: unknown): boolean {
  return (
    err instanceof ApiError &&
    err.status === 409 &&
    err.errors.some((e) => e.code === "IDEMPOTENCY_IN_PROGRESS")
  );
}

/** Shown when {@link isIdempotencyInProgress}: the previous attempt may or may not have landed. */
export const IDEMPOTENCY_IN_PROGRESS_MESSAGE =
  "Poprzednia próba jeszcze trwa albo jej wynik jest nieznany. Sprawdź historię przed ponowieniem.";

/**
 * The same key was sent with a different request body (422): the user edited the form after an
 * attempt whose outcome is unknown. The caller must rotate the key so the edited form can be
 * submitted as a new operation (ADR-111).
 */
export function isIdempotencyKeyReused(err: unknown): boolean {
  return (
    err instanceof ApiError &&
    err.status === 422 &&
    err.errors.some((e) => e.code === "IDEMPOTENCY_KEY_REUSED")
  );
}

/** Shown when {@link isIdempotencyKeyReused}: the edit is fine to send once the history is checked. */
export const IDEMPOTENCY_KEY_REUSED_MESSAGE =
  "Dane zostały zmienione po poprzedniej próbie, której wynik jest nieznany. Sprawdź historię, a potem zapisz ponownie.";

/**
 * Polish copy for the two idempotency failures that need the user to look at the history, or
 * `null` for any other error (the caller shows its own fallback).
 */
export function idempotencyErrorMessage(err: unknown): string | null {
  if (isIdempotencyInProgress(err)) return IDEMPOTENCY_IN_PROGRESS_MESSAGE;
  if (isIdempotencyKeyReused(err)) return IDEMPOTENCY_KEY_REUSED_MESSAGE;
  return null;
}
