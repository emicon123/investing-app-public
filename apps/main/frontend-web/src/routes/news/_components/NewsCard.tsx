import { ExternalLink } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import type { NewsItem, NewsSentiment } from "../../../api/index.js";

interface NewsCardProps {
  item: NewsItem;
}

function sentimentBadgeClass(sentiment: NewsSentiment | null | undefined): string {
  if (sentiment === "POSITIVE") {
    return "border border-neon-gain/30 bg-neon-gain/10 text-neon-gain font-semibold";
  }
  if (sentiment === "NEGATIVE") {
    return "border border-neon-loss/30 bg-neon-loss/10 text-neon-loss font-semibold";
  }
  return "border border-muted-foreground/20 bg-muted/30 text-muted-foreground font-semibold";
}

function sentimentLabel(sentiment: NewsSentiment | null | undefined): string {
  if (sentiment === "POSITIVE") return "Pozytywny";
  if (sentiment === "NEGATIVE") return "Negatywny";
  return "Neutralny";
}

export function NewsCard({ item }: NewsCardProps) {
  return (
    <Card>
      <CardHeader className="space-y-2">
        <div className="flex items-start justify-between gap-3 flex-wrap">
          <div>
            <CardTitle className="text-base font-semibold leading-snug">
              {item.headline}
            </CardTitle>
            <p className="text-xs text-muted-foreground mt-1 font-mono">
              {item.ticker} &middot; {item.assetName}
            </p>
          </div>
          <Badge className={cn("shrink-0", sentimentBadgeClass(item.sentiment))}>
            {sentimentLabel(item.sentiment)}
          </Badge>
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        <p className="text-sm text-muted-foreground whitespace-pre-line">{item.summary}</p>
        {item.sources.length > 0 && (
          <ul className="flex flex-col gap-1">
            {item.sources.map((source) => (
              <li key={source.url}>
                <a
                  href={source.url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-1 text-xs text-neon-cyan hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring rounded"
                >
                  <ExternalLink className="h-3 w-3" aria-hidden="true" />
                  {source.title}
                </a>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
