import { useState, Suspense, type ReactNode } from "react";
import { NavLink, Outlet } from "react-router";
import { Menu, BarChart2, PiggyBank, History, Settings, RefreshCw, ShoppingCart } from "lucide-react";
import { ROUTES } from "@common/app-core";
import { Button } from "./button.js";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "./sheet.js";
import { Skeleton } from "./skeleton.js";
import { ErrorBoundary } from "./ErrorBoundary.js";
import { cn } from "./lib/utils.js";

function PageFallback() {
  return (
    <div className="p-6 space-y-4">
      <Skeleton className="h-8 w-48" />
      <Skeleton className="h-64 w-full" />
    </div>
  );
}

export interface NavItem {
  label: string;
  to: string;
  icon: React.ReactNode;
  /** `NavLink end` matching. Defaults to `to === ROUTES.portfolio` (index route). */
  end?: boolean;
}

export interface NavSection {
  /** Rendered as an eyebrow label above the section — only when the section holds >1 item. */
  title?: string;
  items: NavItem[];
}

const DEFAULT_NAV_SECTIONS: NavSection[] = [
  {
    items: [
      { label: "Portfel", to: ROUTES.portfolio, icon: <BarChart2 className="h-4 w-4" aria-hidden="true" /> },
      { label: "Zakupy", to: ROUTES.purchases, icon: <ShoppingCart className="h-4 w-4" aria-hidden="true" /> },
      { label: "Wpłaty", to: ROUTES.deposits, icon: <PiggyBank className="h-4 w-4" aria-hidden="true" /> },
      { label: "Historia", to: ROUTES.history, icon: <History className="h-4 w-4" aria-hidden="true" /> },
      { label: "Ustawienia", to: ROUTES.settings, icon: <Settings className="h-4 w-4" aria-hidden="true" /> },
    ],
  },
];

function NavLinkItem({ item, onNavigate }: { item: NavItem; onNavigate?: () => void }) {
  return (
    <NavLink
      to={item.to}
      end={item.end ?? item.to === ROUTES.portfolio}
      onClick={onNavigate}
      className={({ isActive }) =>
        cn(
          "flex items-center gap-2 rounded-md px-3 py-2 text-sm transition-colors relative z-10",
          isActive
            ? "nav-active-glow text-accent-foreground font-semibold"
            : "text-muted-foreground hover:bg-accent/40 hover:text-foreground"
        )
      }
    >
      {item.icon}
      {item.label}
    </NavLink>
  );
}

function SidebarNav({ sections, onNavigate }: { sections: NavSection[]; onNavigate?: () => void }) {
  return (
    <nav aria-label="Main navigation" className="flex flex-col gap-6 px-2 py-4">
      {sections.map((section, index) => (
        <div key={section.title ?? `section-${index}`}>
          {section.title && section.items.length > 1 && (
            <p className="eyebrow mb-1 px-3">{section.title}</p>
          )}
          <div className="flex flex-col gap-0.5">
            {section.items.map((item) => (
              <NavLinkItem key={item.to} item={item} onNavigate={onNavigate} />
            ))}
          </div>
        </div>
      ))}
    </nav>
  );
}

/** Status union returned by each app's `useRefreshPrices` hook (from `createUseRefreshPrices`). */
export type RefreshPricesStatus = "idle" | "ok" | "warn" | "err";

export interface UseRefreshPricesResult {
  refresh: () => void;
  isRefreshing: boolean;
  status: RefreshPricesStatus;
}

export interface FxRateLike {
  fromCurrency: string;
  rateToPln: number;
}

export interface UseFxRatesResult {
  data: FxRateLike[] | undefined;
}

interface TopBarProps {
  onMenuClick: () => void;
  menuOpen: boolean;
  topBarExtra?: ReactNode;
  useFxRates: () => UseFxRatesResult;
  useRefreshPrices: () => UseRefreshPricesResult;
}

function TopBar({ onMenuClick, menuOpen, topBarExtra, useFxRates, useRefreshPrices }: TopBarProps) {
  const { refresh, isRefreshing, status: refreshStatus } = useRefreshPrices();

  const { data: fxRatesData } = useFxRates();
  const fxRates = fxRatesData ?? [];
  const usdRate = fxRates.find((r) => r.fromCurrency === "USD")?.rateToPln;
  const eurRate = fxRates.find((r) => r.fromCurrency === "EUR")?.rateToPln;

  const fmt = (v: number) => v.toFixed(4);

  const currentDate = new Intl.DateTimeFormat("pl-PL", {
    weekday: "long",
    year: "numeric",
    month: "long",
    day: "numeric",
  }).format(new Date());

  return (
    <header className="flex h-14 items-center justify-between border-b border-border bg-background/80 backdrop-blur sticky top-0 z-30 px-4 lg:px-6">
      <div className="flex items-center gap-3">
        <button
          className="lg:hidden rounded-md p-1.5 text-muted-foreground hover:bg-accent/40 hover:text-foreground transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          onClick={onMenuClick}
          aria-label={menuOpen ? "Close menu" : "Open menu"}
          aria-expanded={menuOpen}
          aria-controls="mobile-nav"
        >
          <Menu className="h-5 w-5" aria-hidden="true" />
        </button>
        <span className={cn("text-sm text-muted-foreground capitalize", topBarExtra && "hidden md:inline")}>
          {currentDate}
        </span>
        {topBarExtra}
        {(usdRate != null || eurRate != null) && (
          <span className="hidden sm:flex items-center gap-2 text-xs text-muted-foreground font-mono">
            <span className="text-muted-foreground/30">|</span>
            {usdRate != null && (
              <span className="text-neon-cyan font-semibold">USD {fmt(usdRate)}</span>
            )}
            {usdRate != null && eurRate != null && (
              <span className="text-muted-foreground/30">·</span>
            )}
            {eurRate != null && (
              <span className="text-neon-violet font-semibold">EUR {fmt(eurRate)}</span>
            )}
          </span>
        )}
      </div>
      <Button
        variant="outline"
        size="sm"
        onClick={refresh}
        disabled={isRefreshing}
        className={cn(
          "gap-2",
          refreshStatus === "ok" && "border-neon-gain text-neon-gain",
          refreshStatus === "warn" && "border-yellow-400 text-yellow-400",
          refreshStatus === "err" && "border-neon-loss text-neon-loss",
        )}
      >
        <RefreshCw className={cn("h-4 w-4", isRefreshing && "animate-spin")} aria-hidden="true" />
        {isRefreshing
          ? "Odświeżanie…"
          : refreshStatus === "ok"
          ? "Zaktualizowano"
          : refreshStatus === "warn"
          ? "Częściowy błąd"
          : refreshStatus === "err"
          ? "Błąd"
          : "Odśwież ceny"}
      </Button>
    </header>
  );
}

export function NebulaCanvas() {
  return (
    <div className="nebula-canvas" aria-hidden="true">
      <div className="nebula-blob-1" />
      <div className="nebula-blob-2" />
      <div className="nebula-blob-3" />
      <div className="nebula-blob-4" />
      <div className="nebula-blob-5" />
      <div className="conic-halo" />
    </div>
  );
}

export interface AppLayoutBrand {
  /** Required brand text, e.g. "PortfolioB" / "PortfolioC" (forward-compatible with a future `mark` + "Inwestycje" pairing). */
  label: string;
  /** Optional decorative slot rendered before `label` — undefined for portfolioB/portfolioC today. */
  mark?: ReactNode;
}

export interface AppLayoutProps {
  brand: AppLayoutBrand;
  /** Rendered immediately after the top bar's date span, e.g. portfolioC's `<PortfolioSwitcher />`. */
  topBarExtra?: ReactNode;
  useFxRates: () => UseFxRatesResult;
  useRefreshPrices: () => UseRefreshPricesResult;
  /**
   * Navigation sections. Defaults to the flat 5-item list (portfolioB/portfolioC) built off
   * `@common/app-core` ROUTES — pass explicit sections (with `title` + app `icon` nodes)
   * to get investing-app's grouped sidebar instead.
   */
  navSections?: NavSection[];
}

/**
 * Task 170 (design doc §10): the document is the only scroll container. Neither the content
 * column nor `<main>` may be one (`overflow: hidden|auto`), or `position: sticky` (the top bar, the
 * portfolio table's column and group headers) sticks to them instead of the viewport. The column
 * uses `overflow-x: clip`, which still clips sideways overflow but is not a scroll container, and
 * publishes the top bar's height as `--app-sticky-top` for headers that stick below it.
 * The nebula background is off in the shell (slate ground, design doc §14 D14).
 */
export function AppLayout({ brand, topBarExtra, useFxRates, useRefreshPrices, navSections }: AppLayoutProps) {
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);
  const sections = navSections ?? DEFAULT_NAV_SECTIONS;

  return (
    <>
      <div className="flex min-h-screen">
        <aside
          className="hidden lg:sticky lg:top-0 lg:flex lg:h-screen lg:w-[220px] lg:shrink-0 lg:flex-col border-r border-sidebar-border bg-sidebar"
          aria-label="Sidebar"
        >
          {/* Title bar */}
          <div className="flex h-14 items-center gap-2.5 border-b border-sidebar-border px-4">
            <span className="font-display text-xl font-bold tracking-tight gradient-text-tri select-none">
              {brand.mark}
              {brand.label}
            </span>
          </div>

          {/* Nav */}
          <div className="flex-1 overflow-y-auto">
            <SidebarNav sections={sections} />
          </div>
        </aside>

        <Sheet open={mobileMenuOpen} onOpenChange={setMobileMenuOpen}>
          {/* No `position: relative` utility on SheetContent: it would beat the Sheet's `fixed`
              positioning (same cascade layer, same specificity) and drop the drawer below the
              phone viewport. */}
          <SheetContent
            id="mobile-nav"
            side="left"
            className="w-[220px] p-0 border-r border-sidebar-border overflow-hidden bg-sidebar"
          >
            <SheetHeader className="flex h-14 flex-row items-center justify-start gap-2.5 border-b border-sidebar-border px-4">
              <SheetTitle className="font-display text-xl font-bold tracking-tight gradient-text-tri">
                {brand.mark}
                {brand.label}
              </SheetTitle>
            </SheetHeader>
            <div className="flex-1 overflow-y-auto">
              <SidebarNav sections={sections} onNavigate={() => setMobileMenuOpen(false)} />
            </div>
          </SheetContent>
        </Sheet>

        <div className="flex min-w-0 flex-1 flex-col overflow-x-clip [--app-sticky-top:3.5rem]">
          <TopBar
            onMenuClick={() => setMobileMenuOpen(true)}
            menuOpen={mobileMenuOpen}
            topBarExtra={topBarExtra}
            useFxRates={useFxRates}
            useRefreshPrices={useRefreshPrices}
          />
          <main className="min-w-0 flex-1">
            <ErrorBoundary>
              <Suspense fallback={<PageFallback />}>
                <Outlet />
              </Suspense>
            </ErrorBoundary>
          </main>
        </div>
      </div>
    </>
  );
}
