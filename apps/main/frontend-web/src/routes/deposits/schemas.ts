import { z } from "zod";

export const depositFormSchema = z
  .object({
    depositDate: z.string().min(1, { error: "Wymagane" }),
    amount: z.coerce.number<number>().positive({ error: "Kwota musi być dodatnia" }),
    currency: z.enum(["PLN", "EUR", "USD", "CHF", "GBP"]),
    exchange: z.enum(["XTB", "BINANCE"]),
    accountType: z.enum(["IKE", "IKZE", "REGULAR"]),
    portfolio: z.enum(["MAIN", "RETIREMENT"]),
  })
  .superRefine((v, ctx) => {
    if ((v.accountType === "IKE" || v.accountType === "IKZE") && v.currency !== "PLN") {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: "Depozyty IKE/IKZE muszą być w PLN",
        path: ["currency"],
      });
    }
  });

export type DepositFormValues = z.infer<typeof depositFormSchema>;
