import { createUsePurchaseHistory } from "@common/app-core";
import { getPurchaseHistory } from "../api/index.js";
import { queryKeys } from "@/lib/queryKeys";

export const usePurchaseHistory = createUsePurchaseHistory(getPurchaseHistory, () => queryKeys.purchaseHistory.list());
