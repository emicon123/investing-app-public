import { describe, expect, it } from "vitest";
import {
  ApiError,
  IDEMPOTENCY_IN_PROGRESS_MESSAGE,
  IDEMPOTENCY_KEY_REUSED_MESSAGE,
  idempotencyErrorMessage,
  isIdempotencyInProgress,
  isIdempotencyKeyReused,
} from "./errors.js";

describe("isIdempotencyInProgress", () => {
  it("is true for a 409 IDEMPOTENCY_IN_PROGRESS ApiError", () => {
    const err = new ApiError(409, [{ code: "IDEMPOTENCY_IN_PROGRESS", message: "busy" }], "r1");
    expect(isIdempotencyInProgress(err)).toBe(true);
  });

  it("is false for other 409 codes, other statuses and non-ApiErrors", () => {
    expect(isIdempotencyInProgress(new ApiError(409, [{ code: "CONFLICT", message: "x" }], "r"))).toBe(false);
    expect(
      isIdempotencyInProgress(new ApiError(422, [{ code: "IDEMPOTENCY_KEY_REUSED", message: "x" }], "r")),
    ).toBe(false);
    expect(isIdempotencyInProgress(new Error("IDEMPOTENCY_IN_PROGRESS"))).toBe(false);
    expect(isIdempotencyInProgress(undefined)).toBe(false);
  });
});

describe("isIdempotencyKeyReused", () => {
  it("is true for a 422 IDEMPOTENCY_KEY_REUSED ApiError", () => {
    const err = new ApiError(422, [{ code: "IDEMPOTENCY_KEY_REUSED", message: "different body" }], "r1");
    expect(isIdempotencyKeyReused(err)).toBe(true);
  });

  it("is false for other codes, other statuses and non-ApiErrors", () => {
    expect(isIdempotencyKeyReused(new ApiError(422, [{ code: "VALIDATION_ERROR", message: "x" }], "r"))).toBe(false);
    expect(
      isIdempotencyKeyReused(new ApiError(409, [{ code: "IDEMPOTENCY_IN_PROGRESS", message: "x" }], "r")),
    ).toBe(false);
    expect(isIdempotencyKeyReused(new Error("IDEMPOTENCY_KEY_REUSED"))).toBe(false);
    expect(isIdempotencyKeyReused(null)).toBe(false);
  });
});

describe("idempotencyErrorMessage", () => {
  it("maps each idempotency failure to its Polish message", () => {
    const busy = new ApiError(409, [{ code: "IDEMPOTENCY_IN_PROGRESS", message: "busy" }], "r");
    const reused = new ApiError(422, [{ code: "IDEMPOTENCY_KEY_REUSED", message: "diff" }], "r");
    expect(idempotencyErrorMessage(busy)).toBe(IDEMPOTENCY_IN_PROGRESS_MESSAGE);
    expect(idempotencyErrorMessage(reused)).toBe(IDEMPOTENCY_KEY_REUSED_MESSAGE);
  });

  it("is null for anything else, so callers keep their own fallback", () => {
    expect(idempotencyErrorMessage(new ApiError(500, [{ code: "INTERNAL", message: "x" }], "r"))).toBeNull();
    expect(idempotencyErrorMessage(new Error("net"))).toBeNull();
  });
});
