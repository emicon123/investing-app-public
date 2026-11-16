import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { format, parseISO } from "date-fns";
import { pl } from "date-fns/locale";
import { listNews, listNewsDates } from "../../api/index.js";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { PageHeader } from "@/components/PageHeader";
import { queryKeys } from "@/lib/queryKeys";
import { NewsCard } from "./_components/NewsCard";

const STALE_TIME_NEWS_MS = 30 * 60_000;

function formatDateLabel(iso: string): string {
  return format(parseISO(iso), "d MMMM yyyy", { locale: pl });
}

export default function NewsPage() {
  const [selectedDate, setSelectedDate] = useState<string | undefined>(undefined);

  const {
    data: datesData,
    isPending: datesPending,
    isError: datesError,
  } = useQuery({
    queryKey: queryKeys.news.dates(),
    queryFn: () => listNewsDates(),
    staleTime: STALE_TIME_NEWS_MS,
  });

  const availableDates = datesData?.data ?? [];
  const effectiveDate = selectedDate ?? availableDates[0];

  const {
    data: newsData,
    isPending: newsPending,
    isError: newsError,
  } = useQuery({
    queryKey: queryKeys.news.list(effectiveDate),
    queryFn: () => listNews(effectiveDate ? { date: effectiveDate } : undefined),
    staleTime: STALE_TIME_NEWS_MS,
  });

  const newsItems = newsData?.data ?? [];
  const isPending = datesPending || newsPending;

  return (
    <div className="p-6 space-y-6">
      <PageHeader
        eyebrow="Wiadomości"
        title="Wiadomości"
        description="Codzienne podsumowanie newsów dla aktywów w portfelu (AI)"
        actions={
          !datesPending && !datesError && availableDates.length > 0 && (
            <Select
              value={effectiveDate}
              onValueChange={(v) => setSelectedDate(v)}
            >
              <SelectTrigger className="w-56">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {availableDates.map((d) => (
                  <SelectItem key={d} value={d}>
                    {formatDateLabel(d)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )
        }
      />

      {isPending && (
        <div role="status" aria-label="Ładowanie wiadomości" className="grid gap-4 md:grid-cols-2">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-40 w-full rounded-lg" />
          ))}
        </div>
      )}

      {!isPending && (datesError || newsError) && (
        <p role="alert" className="text-destructive text-sm">
          Błąd ładowania wiadomości. Spróbuj odświeżyć stronę.
        </p>
      )}

      {!isPending && !datesError && !newsError && newsItems.length === 0 && (
        <p className="text-muted-foreground text-sm">Brak aktualności na ten dzień.</p>
      )}

      {!isPending && !datesError && !newsError && newsItems.length > 0 && (
        <div className="grid gap-4 md:grid-cols-2">
          {newsItems.map((item) => (
            <NewsCard key={`${item.ticker}-${item.newsDate}`} item={item} />
          ))}
        </div>
      )}
    </div>
  );
}
