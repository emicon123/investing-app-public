import { Suspense, type ComponentType, type ReactNode } from "react";
import { Route, Routes } from "react-router";
import { ROUTES } from "@common/app-core";

/**
 * The five lazy-loaded top-level pages, keyed off the shared `ROUTES` map.
 * Each app owns the concrete `lazy(() => import("./routes/..."))` calls
 * (relative paths that can't resolve the same way from inside this shared
 * package) and passes the resulting components down.
 */
export interface AppShellRoutes {
  portfolio: ComponentType;
  purchases: ComponentType;
  deposits: ComponentType;
  history: ComponentType;
  settings: ComponentType;
}

export interface AppShellExtraRoute {
  /** Route path, e.g. main's `ROUTES.retirement` (`"/retirement"`). */
  path: string;
  /** Pre-built element, e.g. `<RetirementPage />` — mirrors react-router's own
      `<Route path element>` shape, so no new concept to learn. The app owns the
      `lazy(() => import(...))` call (relative paths can't resolve from shared),
      exactly like the five `routes` components today. */
  element: ReactNode;
}

export interface AppShellProps {
  /** App's thin `LoginPage` wrapper (title/login/exchangeGoogleToken already bound). */
  LoginPage: ComponentType;
  /** App's thin `ProtectedRoute` wrapper (isAuthenticated already bound). */
  ProtectedRoute: ComponentType;
  /** App's thin `AppLayout` wrapper (brand/useFxRates/useRefreshPrices already bound). */
  AppLayout: ComponentType;
  routes: AppShellRoutes;
  /** Defaults to `<PortfolioPage />` (portfolioB/portfolioC behavior, unchanged). */
  indexElement?: ReactNode;
  /** Defaults to `[]` (portfolioB/portfolioC behavior, unchanged). Rendered after the five. */
  extraRoutes?: AppShellExtraRoute[];
}

/**
 * Shared route tree: `/login` outside auth, everything else behind
 * `ProtectedRoute` + `AppLayout`. Structure only — every concrete piece
 * (login page, layout, guard, and the five page components) is injected by
 * the app, matching the "shell owns structure, app owns the concrete
 * pieces" shape already used by `AppLayout` and `HistoriaDialog`.
 */
export default function AppShell({
  LoginPage,
  ProtectedRoute,
  AppLayout,
  routes,
  indexElement,
  extraRoutes,
}: AppShellProps) {
  const {
    portfolio: PortfolioPage,
    purchases: PurchasesPage,
    deposits: DepositsPage,
    history: HistoryPage,
    settings: SettingsPage,
  } = routes;

  return (
    <Suspense fallback={null}>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route element={<ProtectedRoute />}>
          <Route element={<AppLayout />}>
            <Route index element={indexElement ?? <PortfolioPage />} />
            <Route path={ROUTES.purchases} element={<PurchasesPage />} />
            <Route path={ROUTES.deposits} element={<DepositsPage />} />
            <Route path={ROUTES.history} element={<HistoryPage />} />
            <Route path={ROUTES.settings} element={<SettingsPage />} />
            {extraRoutes?.map((r) => (
              <Route key={r.path} path={r.path} element={r.element} />
            ))}
          </Route>
        </Route>
      </Routes>
    </Suspense>
  );
}
