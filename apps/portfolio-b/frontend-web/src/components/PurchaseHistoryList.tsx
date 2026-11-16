import { PurchaseHistoryList as SharedPurchaseHistoryList } from "@common/ui-primitives/PurchaseHistoryList";
import { usePurchaseHistory } from "@/hooks/usePurchaseHistory";

export function PurchaseHistoryList() {
  return <SharedPurchaseHistoryList useHistoryQuery={usePurchaseHistory} />;
}
