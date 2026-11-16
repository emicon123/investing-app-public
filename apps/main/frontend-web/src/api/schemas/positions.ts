import { z } from "zod";
import { portfolioTypeSchema, exchangeSchema, assetClassSchema, exposureSchema, currencySchema } from "./enums.js";
import { apiResponseSchema, pagedResponseSchema, entryPriceFieldsShape } from "@common/app-core";

// Jackson's `default-property-inclusion: non_absent` omits null fields from the
// wire entirely rather than sending them as `null` — these two accept either
// shape (absent key or explicit null) and normalize both to `null`, so the
// inferred TS type (and every downstream consumer) stays `| null`, never
// `| undefined`.
const nnum = () => z.number().nullish().transform((v) => v ?? null);
const nstr = () => z.string().nullish().transform((v) => v ?? null);

// Task 156 (Asset / Subasset model). `GET|POST|PUT /positions*` and `/assets/*` all answer with
// the ASSET — the logical group root (key, name, exposure, assetClass, the ONLY holder of the
// targets, group-level `computed` numbers where valuePln = Σ subassets and deviationPct lives on
// the asset) — plus its member SUBASSETS (one concrete holding each, primary first). A subasset
// carries no target field at all. Both portfolios (MAIN, RETIREMENT) exist here, assets are
// per-portfolio, and the asset `key` is an immutable slug that is never joined on.

export const computedPositionFieldsSchema = z.object({
  unitsTotal: nnum(),
  pricePln: nnum(),
  priceNative: nnum(),
  valuePln: nnum(),
  currentPct: nnum(),
  effectiveTargetAllocationPct: nnum(),
  deviationPct: nnum(),
  // profitPct, avgBuyPricePln, avgBuyPriceNative, equivalentUnits, costlessSubassets (task 161) —
  // identical in all three apps, owned by shared-ts.
  ...entryPriceFieldsShape,
});

export const subassetSchema = z.object({
  id: z.number().int(),
  assetId: z.number().int(),
  /** Exactly one subasset per asset is primary = default buy destination. */
  isPrimary: z.boolean(),
  ticker: z.string(),
  name: z.string(),
  portfolio: portfolioTypeSchema,
  exchange: exchangeSchema,
  assetClass: assetClassSchema,
  priceCurrency: currencySchema,
  purchaseCurrency: currencySchema.nullish().transform((v) => v ?? null),
  unitsIke: z.number(),
  unitsIkze: z.number(),
  unitsRegularEur: z.number(),
  unitsRegularUsd: z.number(),
  unitsRegularPln: z.number(),
  effectiveUnits: z.number(),
  totalSpentPln: nnum(),
  /** @deprecated Stale go-live native average (task 161) — parsed for compatibility, never displayed. */
  avgBuyPrice: nnum(),
  yahooTicker: nstr(),
  companyBacked: z.boolean(),
  pricePln: nnum(),
  priceNative: nnum(),
  valuePln: nnum(),
});

export const assetSchema = z.object({
  id: z.number().int(),
  /** Stable logical slug, unique per portfolio (immutable — never derive it from a subasset ticker). */
  key: z.string(),
  name: z.string(),
  portfolio: portfolioTypeSchema,
  exposure: exposureSchema,
  assetClass: assetClassSchema,
  /** Risk-on target (NULL = no target, distinct from an explicit 0 — ADR-053). */
  targetAllocationPct: nnum(),
  /** Risk-off target (ADR-044); null = "same as risk-on". Never read for RETIREMENT. */
  targetAllocationPctRiskOff: nnum(),
  computed: computedPositionFieldsSchema.nullish(),
  // Every asset has >= 1 subasset; tolerate an absent key rather than fail the whole page parse.
  subassets: z.array(subassetSchema).nullish().transform((v) => v ?? []),
  createdAt: z.string().datetime({ offset: true }),
  updatedAt: z.string().datetime({ offset: true }).nullish().transform((v) => v ?? null),
});

/** Inline asset payload for `POST /positions` without `assetId` (targets live here, never on the subasset). */
export const newAssetRequestSchema = z.object({
  name: z.string(),
  exposure: exposureSchema,
  assetClass: assetClassSchema,
  targetAllocationPct: z.number().nullable().optional(),
  targetAllocationPctRiskOff: z.number().nullable().optional(),
});

/**
 * Creates one subasset: under an existing asset of the SAME portfolio (`assetId`, `isPrimary`
 * defaults to false) or under a brand-new asset (`newAsset`; its first subasset is primary).
 * Exactly one of `assetId` / `newAsset` — the backend rejects both/neither with 400, and a
 * top-level `targetAllocationPct` key with 400 as well (targets go inside `newAsset`).
 * `exposure` is optional when attaching (inherited from the asset).
 */
export const createPositionRequestSchema = z.object({
  ticker: z.string(),
  name: z.string(),
  portfolio: portfolioTypeSchema,
  exchange: exchangeSchema,
  assetClass: assetClassSchema,
  exposure: exposureSchema.optional(),
  priceCurrency: currencySchema,
  yahooTicker: z.string().nullable().optional(),
  companyBacked: z.boolean().nullable().optional(),
  assetId: z.number().int().optional(),
  isPrimary: z.boolean().optional(),
  newAsset: newAssetRequestSchema.optional(),
});

/**
 * Subasset-owned fields only. `targetAllocationPct`/`targetAllocationPctRiskOff` are deliberately
 * absent — the backend rejects them with 400; use `updateAssetRequestSchema` (`PUT /assets/{id}`).
 * `assetId` re-homes a NON-primary subasset under another asset of the same portfolio
 * (primary -> 409, unknown asset -> 404, other portfolio -> 400).
 */
export const updatePositionRequestSchema = z.object({
  name: z.string().optional(),
  assetClass: assetClassSchema.optional(),
  yahooTicker: z.string().nullable().optional(),
  companyBacked: z.boolean().nullable().optional(),
  assetId: z.number().int().optional(),
  unitsIke: z.number().optional(),
  unitsIkze: z.number().optional(),
  unitsRegularEur: z.number().optional(),
  unitsRegularUsd: z.number().optional(),
  unitsRegularPln: z.number().optional(),
  totalSpentPln: z.number().nullable().optional(),
  /** @deprecated Stale go-live native average — still accepted by the API, nothing reads it (task 161). */
  avgBuyPrice: z.number().nullable().optional(),
});

/**
 * `PUT /assets/{id}` — the only writer of targets. An omitted key leaves the stored value alone,
 * an explicit `null` CLEARS it (NULL = no target, distinct from 0 = explicit zero, ADR-053).
 * Range 0..100 only — there is deliberately no portfolio Σ == 100 guard (partial edits stay
 * possible; the table's Σ indicator is advisory).
 */
export const updateAssetRequestSchema = z.object({
  name: z.string().optional(),
  targetAllocationPct: z.number().nullable().optional(),
  targetAllocationPctRiskOff: z.number().nullable().optional(),
});

/** `POST /assets/{id}/primary` — flips which subasset is primary (pure boolean swap). */
export const setAssetPrimaryRequestSchema = z.object({
  subassetId: z.number().int(),
});

export const assetResponseSchema = apiResponseSchema(assetSchema);
export const assetListResponseSchema = pagedResponseSchema(assetSchema);

export type ComputedPositionFields = z.infer<typeof computedPositionFieldsSchema>;
export type Subasset = z.infer<typeof subassetSchema>;
export type Asset = z.infer<typeof assetSchema>;
export type NewAssetRequest = z.infer<typeof newAssetRequestSchema>;
export type CreatePositionRequest = z.infer<typeof createPositionRequestSchema>;
export type UpdatePositionRequest = z.infer<typeof updatePositionRequestSchema>;
export type UpdateAssetRequest = z.infer<typeof updateAssetRequestSchema>;
export type SetAssetPrimaryRequest = z.infer<typeof setAssetPrimaryRequestSchema>;
export type AssetResponse = z.infer<typeof assetResponseSchema>;
export type AssetListResponse = z.infer<typeof assetListResponseSchema>;
