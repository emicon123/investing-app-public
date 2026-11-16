import { lazy } from "react";
import { Navigate } from "react-router";
import AppShell from "@common/ui-primitives/AppShell";
import AppLayout from "./components/AppLayout";
import ProtectedRoute from "./components/ProtectedRoute";
import LoginPage from "./pages/LoginPage";
import { ROUTES } from "@/lib/routes";

const PortfolioPage = lazy(() => import("./routes/portfolio/index"));
const DepositsPage = lazy(() => import("./routes/deposits/index"));
const RetirementPage = lazy(() => import("./routes/retirement/index"));
const HistoryPage = lazy(() => import("./routes/history/index"));
const CryptoPage = lazy(() => import("./routes/crypto/index"));
const CryptoTaxPage = lazy(() => import("./routes/crypto/tax"));
const SettingsPage = lazy(() => import("./routes/settings/index"));
const PurchasesPage = lazy(() => import("./routes/purchases/index"));
const RegressionPage = lazy(() => import("./routes/regression/index"));
const TradeHistoryPage = lazy(() => import("./routes/trade-history/index"));
const NewsPage = lazy(() => import("./routes/news/index"));
const BotPage = lazy(() => import("./routes/bot/index"));

export default function App() {
  return (
    <AppShell
      LoginPage={LoginPage}
      ProtectedRoute={ProtectedRoute}
      AppLayout={AppLayout}
      routes={{
        portfolio: PortfolioPage,
        purchases: PurchasesPage,
        deposits: DepositsPage,
        history: HistoryPage,
        settings: SettingsPage,
      }}
      indexElement={<Navigate to={ROUTES.portfolio} replace />}
      extraRoutes={[
        { path: ROUTES.portfolio, element: <PortfolioPage /> },
        { path: ROUTES.retirement, element: <RetirementPage /> },
        { path: ROUTES.crypto, element: <CryptoPage /> },
        { path: ROUTES.cryptoTax, element: <CryptoTaxPage /> },
        { path: ROUTES.regression, element: <RegressionPage /> },
        { path: ROUTES.tradeHistory, element: <TradeHistoryPage /> },
        { path: ROUTES.news, element: <NewsPage /> },
        { path: ROUTES.bot, element: <BotPage /> },
      ]}
    />
  );
}
