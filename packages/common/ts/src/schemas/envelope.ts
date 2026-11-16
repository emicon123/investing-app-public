import { z } from "zod";

export const metaSchema = z.object({
  requestId: z.string(),
  timestamp: z.string().datetime({ offset: true }),
});

export const pageSchema = z.object({
  number: z.number().int(),
  size: z.number().int(),
  total: z.number().int(),
});

export function apiResponseSchema<T extends z.ZodTypeAny>(dataSchema: T) {
  return z.object({
    data: dataSchema,
    meta: metaSchema,
  });
}

export function pagedResponseSchema<T extends z.ZodTypeAny>(itemSchema: T) {
  return z.object({
    data: z.array(itemSchema),
    meta: metaSchema,
    page: pageSchema,
  });
}

export type Meta = z.infer<typeof metaSchema>;
export type PageInfo = z.infer<typeof pageSchema>;
