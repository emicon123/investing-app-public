import { lazy } from "react";
import AppShell from "@common/ui-primitives/AppShell";
import AppLayout from "./components/AppLayout";
import ProtectedRoute from "./components/ProtectedRoute";
import LoginPage from "./pages/LoginPage";

const PortfolioPage = lazy(() => import("./routes/portfolio/index"));
const RebalancingPage = lazy(() => import("./routes/purchases/index"));
const DepositsPage = lazy(() => import("./routes/deposits/index"));
const HistoryPage = lazy(() => import("./routes/history/index"));
const SettingsPage = lazy(() => import("./routes/settings/index"));

export default function App() {
  return (
    <AppShell
      LoginPage={LoginPage}
      ProtectedRoute={ProtectedRoute}
      AppLayout={AppLayout}
      routes={{
        portfolio: PortfolioPage,
        purchases: RebalancingPage,
        deposits: DepositsPage,
        history: HistoryPage,
        settings: SettingsPage,
      }}
    />
  );
}
