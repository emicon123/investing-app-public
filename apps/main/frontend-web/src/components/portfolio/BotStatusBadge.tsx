import { Link } from "react-router";
import { useQuery } from "@tanstack/react-query";
import { cn } from "@common/ui-primitives/lib/utils";
import { getBotOpenPositions, getBotPlanLatest, listBotCalendar } from "../../api/index.js";
import { openPositionsSummary } from "./botOpenPositionsSummary";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { queryKeys } from "@/lib/queryKeys";
import { ROUTES } from "@/lib/routes";

/**
 * The OKX bot's state in the portfolio header (task 170, design doc §14 D1): a compact link to
 * /bot instead of the old full-width widget. The next two calendar events and the latest plan's
 * summary sit in a tooltip (opens on hover and on keyboard focus). Same queries as the widget.
 */

const DAY_MS = 24 * 60 * 60 * 1000;

function useBotCalendarWeek() {
  const today = new Date();
  const params = {
    from: today.toISOString().slice(0, 10),
    to: new Date(today.getTime() + 7 * DAY_MS).toISOString().slice(0, 10),
    impact: "HIGH,MEDIUM",
    currency: "USD",
    size: 100,
  } as const;
  return useQuery({ queryKey: queryKeys.bot.calendar(params), queryFn: () => listBotCalendar(params), staleTime: 60_000 });
}

export function BotStatusBadge() {
  const open = useQuery({ queryKey: queryKeys.bot.openPositions(), queryFn: () => getBotOpenPositions(), staleTime: 10_000 });
  const calendar = useBotCalendarWeek();
  const plan = useQuery({ queryKey: queryKeys.bot.planLatest(), queryFn: getBotPlanLatest, staleTime: 60_000, retry: false });

  const healthy = open.isSuccess;
  const summary = open.isError ? "brak danych" : open.isPending ? "…" : openPositionsSummary(open.data.data);
  const events = calendar.data?.data.slice(0, 2) ?? [];
  const planSummary = plan.data?.data.summary ?? null;

  return (
    <TooltipProvider delayDuration={150}>
      <Tooltip>
        <TooltipTrigger asChild>
          <Link
            to={ROUTES.bot}
            className={cn(
              "inline-flex min-h-8 items-center gap-2 rounded-full border px-3 text-[13px] text-text-soft no-underline transition-colors motion-reduce:transition-none pointer-coarse:min-h-11",
              "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring",
              healthy ? "border-gain/25 bg-gain/6 hover:bg-gain/12" : "border-hairline-strong hover:bg-white/6",
            )}
          >
            <span
              aria-hidden="true"
              className={cn(
                "size-[7px] rounded-full",
                healthy ? "bg-gain shadow-[0_0_0_3px_rgba(16,185,129,0.18)]" : "bg-status-ok",
              )}
            />
            <span className="font-medium text-text-1">Bot OKX</span>
            <span className="text-text-2">{summary}</span>
          </Link>
        </TooltipTrigger>
        <TooltipContent className="max-w-80 space-y-1.5 text-xs">
          {events.length > 0 ? (
            <>
              <p className="font-medium">Nadchodzące:</p>
              {events.map((e) => (
                <p key={e.id} className="flex justify-between gap-3">
                  <span>
                    {e.eventName} {new Date(e.eventTime).toLocaleDateString("pl-PL")} · F: {e.forecast ?? "—"}
                  </span>
                  <span className="text-text-2">{e.impact}</span>
                </p>
              ))}
            </>
          ) : (
            <p>Brak wydarzeń w najbliższym tygodniu.</p>
          )}
          {planSummary && <p className="line-clamp-3 text-text-2">Plan: {planSummary}</p>}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
