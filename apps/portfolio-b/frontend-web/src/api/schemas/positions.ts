import { z } from "zod";
import { exchangeSchema, assetClassSchema, exposureSchema, currencySchema } from "./enums.js";
import { apiResponseSchema, pagedResponseSchema, entryPriceFieldsShape } from "@common/app-core";

// Jackson's `default-property-inclusion: non_absent` omits null fields from the
// wire entirely rather than sending them as `null` — these two accept either
// shape (absent key or explicit null) and normalize both to `null`, so the
// inferred TS type (and every downstream consumer) stays `| null`, never
// `| undefined`.
const nnum = () => z.number().nullish().transform((v) => v ?? null);
const nstr = () => z.string().nullish().transform((v) => v ?? null);

export const computedPositionFieldsSchema = z.object({
  unitsTotal: nnum(),
  pricePln: nnum(),
  priceNative: nnum(),
  valuePln: nnum(),
  currentPct: nnum(),
  deviationPct: nnum(),
  // profitPct, avgBuyPricePln, avgBuyPriceNative, equivalentUnits, costlessSubassets (task 161) —
  // identical in all three apps, owned by shared-ts.
  ...entryPriceFieldsShape,
});

// Task 156 (Asset / Subasset model). `GET|POST|PUT /positions*` and `/assets/*` all answer with
// the ASSET (the logical group root: key, name, exposure, assetClass, the ONLY holder of the
// target, group-level `computed` numbers) plus its member SUBASSETS (one concrete holding each,
// primary first). Targets exist on the Asset only — a subasset carries no target field at all.

export const subassetSchema = z.object({
  id: z.number().int(),
  assetId: z.number().int(),
  /** Exactly one subasset per asset is primary = default buy destination. */
  isPrimary: z.boolean(),
  ticker: z.string(),
  name: z.string(),
  exchange: exchangeSchema,
  yahooTicker: nstr(),
  priceCurrency: currencySchema,
  unitsIkeTata: z.number(),
  unitsIkeMama: z.number(),
  unitsIkzeTata: z.number(),
  unitsIkzeMama: z.number(),
  unitsInne: z.number(),
  effectiveUnits: z.number(),
  pricePln: nnum(),
  priceNative: nnum(),
  valuePln: nnum(),
});

export const assetSchema = z.object({
  id: z.number().int(),
  /** Stable slug (the first subasset's ticker at creation; immutable). */
  key: z.string(),
  name: z.string(),
  assetClass: assetClassSchema,
  exposure: exposureSchema,
  targetAllocationPct: nnum(),
  computed: computedPositionFieldsSchema.optional(),
  // Every asset has >= 1 subasset; tolerate an absent key rather than fail the whole page parse.
  subassets: z.array(subassetSchema).nullish().transform((v) => v ?? []),
  createdAt: z.string().datetime({ offset: true }),
  updatedAt: z.string().datetime({ offset: true }),
});

/** Inline asset payload for `POST /positions` without `assetId` (targets live here, not on the subasset). */
export const newAssetRequestSchema = z.object({
  name: z.string(),
  exposure: exposureSchema,
  assetClass: assetClassSchema,
  targetAllocationPct: z.number().optional(),
});

/**
 * Creates one subasset: under an existing asset (`assetId` set; `isPrimary` defaults to false) or
 * under a brand-new asset (`newAsset` set, `assetId` omitted; first subasset is primary). Exactly
 * one of `assetId` / `newAsset` — the backend rejects both/neither with 400.
 */
export const createPositionRequestSchema = z.object({
  ticker: z.string(),
  name: z.string(),
  exchange: exchangeSchema,
  assetClass: assetClassSchema,
  exposure: exposureSchema,
  priceCurrency: currencySchema,
  yahooTicker: z.string().nullable().optional(),
  assetId: z.number().int().optional(),
  isPrimary: z.boolean().optional(),
  newAsset: newAssetRequestSchema.optional(),
});

/**
 * Subasset-owned fields only. `targetAllocationPct` is deliberately absent — the backend rejects
 * it with 400; use `updateAssetRequestSchema` (`PUT /assets/{id}`). `assetId` re-homes a
 * NON-primary subasset under another asset (primary -> 409).
 */
export const updatePositionRequestSchema = z.object({
  name: z.string().optional(),
  groupName: z.string().nullable().optional(),
  assetClass: assetClassSchema.optional(),
  yahooTicker: z.string().nullable().optional(),
  assetId: z.number().int().optional(),
  unitsIkeTata: z.number().optional(),
  unitsIkeMama: z.number().optional(),
  unitsIkzeTata: z.number().optional(),
  unitsIkzeMama: z.number().optional(),
  unitsInne: z.number().optional(),
  totalSpentPln: z.number().nullable().optional(),
  /** @deprecated Stale go-live native average — still accepted by the API, nothing reads it (task 161). */
  avgBuyPrice: z.number().nullable().optional(),
});

/** `PUT /assets/{id}` — the only writer of targets (guarded by the Σ targets == 100 invariant). */
export const updateAssetRequestSchema = z.object({
  name: z.string().optional(),
  targetAllocationPct: z.number().nullable().optional(),
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
