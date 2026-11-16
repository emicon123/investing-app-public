import { z } from "zod";
import { apiResponseSchema } from "@common/app-core";

export const newsSentimentSchema = z.enum(["POSITIVE", "NEUTRAL", "NEGATIVE"]);

export const newsSourceSchema = z.object({
  title: z.string(),
  url: z.string(),
});

export const newsItemSchema = z.object({
  id: z.number().int().optional(),
  ticker: z.string(),
  assetName: z.string(),
  headline: z.string(),
  summary: z.string(),
  sentiment: newsSentimentSchema.nullable().optional(),
  sources: z.array(newsSourceSchema),
  newsDate: z.string(),
  createdAt: z.string().datetime({ offset: true }).optional(),
  updatedAt: z.string().datetime({ offset: true }).optional(),
});

export const newsListResponseSchema = apiResponseSchema(z.array(newsItemSchema));

export const newsDatesListResponseSchema = apiResponseSchema(z.array(z.string()));

export type NewsSentiment = z.infer<typeof newsSentimentSchema>;
export type NewsSource = z.infer<typeof newsSourceSchema>;
export type NewsItem = z.infer<typeof newsItemSchema>;
export type NewsListResponse = z.infer<typeof newsListResponseSchema>;
export type NewsDatesListResponse = z.infer<typeof newsDatesListResponseSchema>;
