import { z } from "zod";

export const depositFormSchema = z.object({
  depositDate: z.string().min(1, "Wymagane"),
  amount: z.coerce.number<number>().positive("Kwota musi być dodatnia"),
  comment: z.string().optional(),
});

export type DepositFormValues = z.infer<typeof depositFormSchema>;
