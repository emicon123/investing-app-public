import { z } from "zod";
import { apiResponseSchema } from "@common/app-core";

export const settingSchema = z.object({
  key: z.string(),
  value: z.string(),
  updatedAt: z.string().datetime({ offset: true }),
});

export const upsertSettingRequestSchema = z.object({
  value: z.string(),
});

export const settingResponseSchema = apiResponseSchema(settingSchema);
export const settingsListResponseSchema = apiResponseSchema(z.array(settingSchema));

export type Setting = z.infer<typeof settingSchema>;
export type UpsertSettingRequest = z.infer<typeof upsertSettingRequestSchema>;
export type SettingResponse = z.infer<typeof settingResponseSchema>;
export type SettingsListResponse = z.infer<typeof settingsListResponseSchema>;
