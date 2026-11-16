import { z } from "zod";

export const tradeFormSchema = z.object({
  tradeDate: z.string().min(1, { error: "Wymagane" }),
  ticker: z.string().min(1, { error: "Wymagane" }),
  side: z.enum(["BUY", "SELL"]),
  quantity: z.coerce.number<number>().positive({ error: "Musi być > 0" }),
  unitPrice: z.coerce.number<number>().positive({ error: "Musi być > 0" }),
  priceCurrency: z.enum(["PLN", "EUR", "USD", "CHF", "GBP"]),
  totalAmount: z.coerce.number<number>().positive({ error: "Musi być > 0" }),
  amountPln: z.coerce.number<number>().positive({ error: "Musi być > 0" }),
  exchange: z.enum(["XTB", "BINANCE"]),
  accountType: z.enum(["IKE", "IKZE", "REGULAR"]),
  notes: z.string().optional(),
});

export type TradeFormValues = z.infer<typeof tradeFormSchema>;
