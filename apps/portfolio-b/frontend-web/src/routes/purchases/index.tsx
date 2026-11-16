import { AccountCashCard } from "@/components/AccountCashCard";
import { RebalancingPanel } from "@/components/RebalancingPanel";
import { PurchaseHistoryList } from "@/components/PurchaseHistoryList";
import { PageHeader } from "@common/ui-primitives/PageHeader";

export default function RebalancingPage() {
  return (
    <div className="p-6 space-y-6">
      <PageHeader title="Zakupy" />
      <AccountCashCard />
      <RebalancingPanel />
      <section className="space-y-3">
        <h2 className="text-lg font-semibold">Historia zakupów</h2>
        <PurchaseHistoryList />
      </section>
    </div>
  );
}
