/**
 * Single-threshold zigzag peak/trough dating (ADR-072) — derives bull/bear
 * regime segments from a raw daily close series. Pure function, no
 * React/recharts import, so it's trivially unit-testable against a synthetic
 * close series and reusable outside the chart if ever needed.
 */

export type PriceRegimeType = "bull" | "bear";

export interface PriceRegimeSegment {
  type: PriceRegimeType;
  startTs: number;
  endTs: number;
}

interface RegimePoint {
  ts: number;
  close: number;
}

/**
 * Walks `points` once, tracking a single pending extreme (a running high
 * while scanning for a peak, a running low while scanning for a trough).
 * A confirmed turning point requires a >= thresholdPct reversal from that
 * pending extreme — this is what suppresses whipsaw from short-lived wiggles,
 * so no separate minimum-duration filter is applied on top (ADR-072 §1).
 *
 * Two edge behaviors are intentional, not bugs (see ADR-072 §1):
 * - points before `direction` is first established belong to no segment and
 *   are simply not covered by any emitted segment (leading gap).
 * - the final segment (last confirmed anchor -> last point) is always
 *   emitted even though its end isn't a confirmed turning point yet
 *   (trailing provisional segment) — otherwise the most recent months would
 *   render unshaded.
 */
export function computeZigzagRegimes(
  points: RegimePoint[],
  thresholdPct = 0.2
): PriceRegimeSegment[] {
  if (points.length < 2) return [];

  const segments: PriceRegimeSegment[] = [];

  let anchorIdx = 0; // start of the segment currently being built
  let pendingIdx = 0;
  let pendingClose = points[0]!.close;
  let direction: "up" | "down" | null = null;

  for (let i = 1; i < points.length; i++) {
    const close = points[i]!.close;

    if (direction == null) {
      if (close >= pendingClose * (1 + thresholdPct)) {
        direction = "up";
        pendingIdx = i;
        pendingClose = close;
      } else if (close <= pendingClose * (1 - thresholdPct)) {
        direction = "down";
        pendingIdx = i;
        pendingClose = close;
      }
      // else: still undetermined, keep scanning from points[0]
      continue;
    }

    if (direction === "up") {
      if (close > pendingClose) {
        pendingIdx = i; // new running high
        pendingClose = close;
      } else if (close <= pendingClose * (1 - thresholdPct)) {
        // 20% off the high -> confirmed peak
        segments.push({ type: "bull", startTs: points[anchorIdx]!.ts, endTs: points[pendingIdx]!.ts });
        anchorIdx = pendingIdx;
        direction = "down";
        pendingIdx = i;
        pendingClose = close;
      }
    } else {
      if (close < pendingClose) {
        pendingIdx = i; // new running low
        pendingClose = close;
      } else if (close >= pendingClose * (1 + thresholdPct)) {
        // 20% off the low -> confirmed trough
        segments.push({ type: "bear", startTs: points[anchorIdx]!.ts, endTs: points[pendingIdx]!.ts });
        anchorIdx = pendingIdx;
        direction = "up";
        pendingIdx = i;
        pendingClose = close;
      }
    }
  }

  // Trailing, unconfirmed segment from the last confirmed turning point to
  // the last available data point, typed by the current (in-progress)
  // direction.
  if (direction != null && anchorIdx < points.length - 1) {
    segments.push({
      type: direction === "up" ? "bull" : "bear",
      startTs: points[anchorIdx]!.ts,
      endTs: points[points.length - 1]!.ts,
    });
  }

  return segments;
}
