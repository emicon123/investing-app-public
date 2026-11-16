import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { PageHeader } from "@/components/PageHeader";
import { TradeEntryForm } from "./_components/TradeEntryForm";
import { TradesHistoryTable } from "./_components/TradesHistoryTable";
import { BinanceTransfersTable } from "./_components/BinanceTransfersTable";

export default function CryptoPage() {
  return (
    <div className="p-6 space-y-4">
      <PageHeader eyebrow="Krypto" title="Transakcje" />
      <Tabs defaultValue="binance-deposits">
        <TabsList>
          <TabsTrigger value="binance-deposits">Wpłaty na Binance</TabsTrigger>
          <TabsTrigger value="add">Dodaj transakcję</TabsTrigger>
          <TabsTrigger value="history">Historia</TabsTrigger>
        </TabsList>
        <TabsContent value="binance-deposits" className="pt-4">
          <BinanceTransfersTable />
        </TabsContent>
        <TabsContent value="add" className="pt-4">
          <TradeEntryForm />
        </TabsContent>
        <TabsContent value="history" className="pt-4">
          <TradesHistoryTable />
        </TabsContent>
      </Tabs>
    </div>
  );
}
