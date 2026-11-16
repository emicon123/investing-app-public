import { z } from "zod";

export const createSessionSchema = z.object({
  portfoliosIncluded: z.enum(["BOTH", "MAIN", "RETIREMENT"]),
  cashOverridePln: z.preprocess(
    (val) => (val === "" || val == null ? undefined : Number(val)),
    z.number().min(0).optional()
  ),
  notes: z.string().max(500).optional(),
});

export type CreateSessionFormValues = z.infer<typeof createSessionSchema>;
