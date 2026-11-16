import { useState } from "react";
import { resolveAssetIcon } from "@common/app-core";
import { cn } from "../lib/utils.js";
import { displayTicker } from "./model/portfolioFormat.js";

/** 1–2 letters of the display ticker, the fallback when no icon resolves or the image fails. */
function monogramOf(ticker: string): string {
  return displayTicker(ticker).replace(/[^A-Za-z0-9]/g, "").slice(0, 2).toUpperCase();
}

function Monogram({ ticker }: { ticker: string }) {
  return (
    <span
      aria-hidden="true"
      data-testid="asset-monogram"
      className="flex size-[30px] shrink-0 items-center justify-center rounded-full bg-muted text-[11px] font-semibold text-text-2"
    >
      {monogramOf(ticker)}
    </span>
  );
}

/**
 * A position's 30 px round icon (task 170, design doc §9). Decorative (`alt=""`): the name is in
 * the text next to it. Favicons are drawn padded on a white circle; an unresolved ticker or an
 * image that fails to load shows a monogram instead of disappearing.
 */
export function AssetIcon({ ticker, assetClass }: { ticker: string; assetClass: string }) {
  const spec = resolveAssetIcon(ticker, assetClass);
  const [failedSrc, setFailedSrc] = useState<string | null>(null);

  if (spec === null || failedSrc === spec.src) return <Monogram ticker={ticker} />;

  return (
    <img
      src={spec.src}
      alt=""
      width={30}
      height={30}
      loading="lazy"
      onError={() => setFailedSrc(spec.src)}
      className={cn(
        "size-[30px] shrink-0 rounded-full object-contain",
        spec.kind === "favicon" && "bg-white p-[5px]",
      )}
    />
  );
}
