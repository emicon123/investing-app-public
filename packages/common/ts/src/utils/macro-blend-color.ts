/**
 * Blue↔orange color-by-value scale for the macro blend-factor history chart
 * (ADR-069). `blendFactor` is 0 (risk-off) .. 1 (risk-on) — see
 * `MacroStatePanel.tsx`'s `formatBlendFactor` for the same orientation
 * (flipped by ADR-071).
 *
 * The two anchors are the dataviz skill's own documented categorical
 * palette slots 1 (blue) and 2 (orange), dark-mode steps — already tuned to
 * sit inside the dark OKLCH lightness band and clear 3:1 contrast on a dark
 * surface, and (checked separately) they also clear 3.6:1+ on a white
 * surface, so one fixed pair works for both themes without a light/dark
 * branch — the same call this codebase already makes for e.g. the
 * trade-history price line's hardcoded "#3b82f6".
 *
 * Interpolation is linear in OKLab (not hue/HSL rotation): the two anchors
 * sit at nearly identical OKLCH lightness (L≈0.622) but ~215° apart in hue,
 * so a straight line between their OKLab a/b coordinates passes close to
 * the neutral (near-zero chroma) axis around the midpoint — i.e. it
 * self-produces the "two hues + a neutral gray midpoint" diverging
 * structure the dataviz skill calls for, without a rainbow sweep through
 * unrelated hues and without a separate hand-picked gray stop. Verified
 * numerically (not eyeballed): midpoint chroma drops to ~0.05 (from ~0.17
 * at the poles) and contrast against both the app's dark surface and white
 * stays >=3.6:1 across the whole 0..1 range.
 */

const BLUE_RISK_OFF = "#3987e5"; // blend -> 0
const ORANGE_RISK_ON = "#d95926"; // blend -> 1

// -- sRGB <-> OKLab, mirrors dataviz skill's scripts/validate_palette.js --

function hexToLinearRgb(hex: string): [number, number, number] {
  const h = hex.replace(/^#/, "");
  const toLinear = (c: number) => (c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);
  return [0, 2, 4].map((i) => toLinear(parseInt(h.slice(i, i + 2), 16) / 255)) as [
    number,
    number,
    number,
  ];
}

function linearToSrgbByte(c: number): number {
  const clamped = Math.max(0, Math.min(1, c));
  const s = clamped <= 0.0031308 ? 12.92 * clamped : 1.055 * clamped ** (1 / 2.4) - 0.055;
  return Math.round(Math.max(0, Math.min(1, s)) * 255);
}

function oklabFromLinearRgb([r, g, b]: [number, number, number]): [number, number, number] {
  const l = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b);
  const m = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b);
  const s = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b);
  return [
    0.2104542553 * l + 0.7936177850 * m - 0.0040720468 * s,
    1.9779984951 * l - 2.4285922050 * m + 0.4505937099 * s,
    0.0259040371 * l + 0.7827717662 * m - 0.8086757660 * s,
  ];
}

function linearRgbFromOklab([L, a, b]: [number, number, number]): [number, number, number] {
  const l_ = L + 0.3963377774 * a + 0.2158037573 * b;
  const m_ = L - 0.1055613458 * a - 0.0638541728 * b;
  const s_ = L - 0.0894841775 * a - 1.2914855480 * b;
  const l = l_ ** 3;
  const m = m_ ** 3;
  const s = s_ ** 3;
  return [
    4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s,
    -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s,
    -0.0041960863 * l - 0.7034186147 * m + 1.7076147010 * s,
  ];
}

function hexFromOklab(lab: [number, number, number]): string {
  const rgb = linearRgbFromOklab(lab).map(linearToSrgbByte);
  return `#${rgb.map((v) => v.toString(16).padStart(2, "0")).join("")}`;
}

const BLUE_LAB = oklabFromLinearRgb(hexToLinearRgb(BLUE_RISK_OFF));
const ORANGE_LAB = oklabFromLinearRgb(hexToLinearRgb(ORANGE_RISK_ON));

/**
 * Maps a blend factor (0..1, clamped) to a hex color on the blue(risk-off)
 * <-> orange(risk-on) scale, for use as a per-point `<stop stop-color>` in
 * an SVG gradient (or any other per-value fill/stroke need).
 */
export function blendToColor(blend: number): string {
  const t = Math.max(0, Math.min(1, blend));
  const lab: [number, number, number] = [
    BLUE_LAB[0] + (ORANGE_LAB[0] - BLUE_LAB[0]) * t,
    BLUE_LAB[1] + (ORANGE_LAB[1] - BLUE_LAB[1]) * t,
    BLUE_LAB[2] + (ORANGE_LAB[2] - BLUE_LAB[2]) * t,
  ];
  return hexFromOklab(lab);
}
