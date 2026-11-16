import { PurchaseHistoryList as SharedPurchaseHistoryList } from "@common/ui-primitives/PurchaseHistoryList";
import { usePurchaseHistory } from "@/hooks/usePurchaseHistory";
import { usePortfolio } from "@/lib/portfolio";

function useHistoryQuery() {
  const { portfolio } = usePortfolio();
  return usePurchaseHistory(portfolio);
}

export function PurchaseHistoryList() {
  return <SharedPurchaseHistoryList useHistoryQuery={useHistoryQuery} />;
}
