import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { formatPln } from "@common/app-core";
import { getAccountCash, saveAccountCash } from "../api/index.js";
import type { AccountCash, AccountCashUpdate } from "../api/index.js";
import { Button } from "@/components/ui/button";
import { DecimalInput } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

const ACCOUNT_LABELS: Record<keyof AccountCashUpdate, string> = {
  ikeTata:  "IKE tata",
  ikeMama:  "IKE mama",
  ikzeTata: "IKZE tata",
  ikzeMama: "IKZE mama",
};

// Draft is kept as raw strings while typing — coercing through Number() on
// every keystroke (as the previous number-typed draft did) drops a trailing
// "." and makes decimals impossible to enter. Coerce to numbers only where a
// number is actually needed (derived total, mutation payload).
type AccountCashDraft = Record<keyof AccountCashUpdate, string>;

const ZERO: AccountCashDraft = { ikeTata: "", ikeMama: "", ikzeTata: "", ikzeMama: "" };

function toNumber(value: string): number {
  const n = Number(value);
  return Number.isFinite(n) ? n : 0;
}

function formatReason(reason: string | null | undefined) {
  if (reason === "POST_PURCHASE") return "po zakupach";
  return "ręcznie";
}

export function AccountCashCard() {
  const queryClient = useQueryClient();

  const { data: stored } = useQuery<AccountCash>({
    queryKey: ["accountCash"],
    queryFn: getAccountCash,
  });

  const [draft, setDraft] = useState<AccountCashDraft>(ZERO);

  // Re-fill the draft whenever the stored amounts change (first load, or a refetch after a
  // purchase). Adjusted during render (React's "reset state when a prop changes" pattern) rather
  // than in an effect: same end state without an extra commit. A re-render with the same `stored`
  // object leaves what the user typed alone.
  const [syncedFrom, setSyncedFrom] = useState<AccountCash | undefined>(undefined);
  if (stored && stored !== syncedFrom) {
    setSyncedFrom(stored);
    setDraft({
      ikeTata: stored.ikeTata === 0 ? "" : String(stored.ikeTata),
      ikeMama: stored.ikeMama === 0 ? "" : String(stored.ikeMama),
      ikzeTata: stored.ikzeTata === 0 ? "" : String(stored.ikzeTata),
      ikzeMama: stored.ikzeMama === 0 ? "" : String(stored.ikzeMama),
    });
  }

  const saveMutation = useMutation({
    mutationFn: saveAccountCash,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["accountCash"] }),
  });

  const numericDraft: AccountCashUpdate = {
    ikeTata: toNumber(draft.ikeTata),
    ikeMama: toNumber(draft.ikeMama),
    ikzeTata: toNumber(draft.ikzeTata),
    ikzeMama: toNumber(draft.ikzeMama),
  };
  const total = numericDraft.ikeTata + numericDraft.ikeMama + numericDraft.ikzeTata + numericDraft.ikzeMama;

  return (
    <Card>
      <CardHeader className="pb-3">
        <div className="flex items-center justify-between">
          <CardTitle className="text-base">Dostępna gotówka na kontach</CardTitle>
          {stored?.updatedAt && (
            <span className="text-xs text-muted-foreground">
              Aktualizacja: {new Date(stored.updatedAt).toLocaleString("pl-PL", { dateStyle: "short", timeStyle: "short" })} ({formatReason(stored.updateReason)})
            </span>
          )}
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="grid grid-cols-2 gap-x-6 gap-y-3 sm:grid-cols-4">
          {(Object.keys(ACCOUNT_LABELS) as (keyof AccountCashUpdate)[]).map((key) => (
            <div key={key} className="flex flex-col gap-1">
              <Label htmlFor={`cash-${key}`} className="text-xs text-muted-foreground">
                {ACCOUNT_LABELS[key]}
              </Label>
              <DecimalInput
                id={`cash-${key}`}
                value={draft[key]}
                placeholder="0"
                onChange={(e) => setDraft({ ...draft, [key]: e.target.value })}
                className="h-8 text-sm w-full"
              />
            </div>
          ))}
        </div>
        <div className="flex items-center gap-3">
          <span className="text-sm text-muted-foreground">
            Łącznie: <span className="font-semibold text-foreground">{formatPln(total)}</span>
          </span>
          <Button
            variant="outline"
            size="sm"
            onClick={() => saveMutation.mutate(numericDraft)}
            disabled={saveMutation.isPending}
          >
            {saveMutation.isPending ? "Zapisywanie..." : "Zapisz salda"}
          </Button>
          {saveMutation.isSuccess && (
            <span className="text-xs text-green-600">Zapisano</span>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
