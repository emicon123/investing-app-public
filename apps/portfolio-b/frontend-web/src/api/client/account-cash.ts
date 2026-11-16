import { apiClient } from "@common/app-core";
import { accountCashSchema, type AccountCash, type AccountCashUpdate } from "../schemas/index.js";

export async function getAccountCash(): Promise<AccountCash> {
  const r = await apiClient.get("/account-cash");
  return accountCashSchema.parse(r.data);
}

export async function saveAccountCash(data: AccountCashUpdate): Promise<AccountCash> {
  const r = await apiClient.put("/account-cash", data);
  return accountCashSchema.parse(r.data);
}

export async function deductAccountCash(data: AccountCashUpdate): Promise<AccountCash> {
  const r = await apiClient.post("/account-cash/deduct", data);
  return accountCashSchema.parse(r.data);
}
