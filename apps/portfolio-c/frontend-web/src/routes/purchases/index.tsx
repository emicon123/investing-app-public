import { RebalancingPanel } from "@/components/RebalancingPanel";
import { PurchaseHistoryList } from "@/components/PurchaseHistoryList";
import { usePortfolio } from "@/lib/portfolio";
import { PORTFOLIO_LABELS } from "@/lib/formConstants";
import { PageHeader } from "@common/ui-primitives/PageHeader";

export default function RebalancingPage() {
  const { portfolio } = usePortfolio();

  return (
    <div className="p-6 space-y-6">
      <PageHeader title={`Zakupy — ${PORTFOLIO_LABELS[portfolio]}`} />
      <RebalancingPanel />
      <section className="space-y-3">
        <h2 className="text-lg font-semibold">Historia zakupów</h2>
        <PurchaseHistoryList />
      </section>
    </div>
  );
}
