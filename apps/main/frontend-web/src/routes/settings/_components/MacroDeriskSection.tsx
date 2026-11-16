import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { updateSetting, type Setting } from "../../../api/index.js";

import { Card, CardContent } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { queryKeys } from "@/lib/queryKeys";

const ENABLED_KEY = "rebalancing.macro_derisk_enabled";

function findSetting(settings: Setting[], key: string): Setting | undefined {
  return settings.find((s) => s.key === key);
}

export function MacroDeriskSection({ settings }: { settings: Setting[] }) {
  const queryClient = useQueryClient();

  const enabledSetting = findSetting(settings, ENABLED_KEY);

  const [enabled, setEnabled] = useState<boolean>(
    enabledSetting?.value !== "false"
  );

  const mutation = useMutation({
    mutationFn: ({ key, value }: { key: string; value: string }) =>
      updateSetting(key, { value }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.settings.all });
      toast.success("Ustawienia de-risk zapisane");
    },
    onError: () => {
      toast.error("Błąd zapisu ustawień de-risk");
    },
  });

  function handleToggle(next: boolean) {
    setEnabled(next);
    mutation.mutate({ key: ENABLED_KEY, value: String(next) });
  }

  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">Makro de-risk (rebalancing)</h2>
      <p className="text-sm text-muted-foreground">
        Od ADR-044 cele MAIN interpolują się w sposób ciągły między tabelą
        risk-on a risk-off na podstawie współczynnika blend (0–1), liczonego z
        wygładzonego wyniku Z płynności netto i Global M2 — bez skokowych cięć.
        Formuła blendu jest ustalona/skalibrowana i nie ma tu suwaka do jej
        edycji; poniższy przełącznik włącza lub wyłącza cały mechanizm.
      </p>

      <Card>
        <CardContent className="pt-6">
          <div className="flex items-center justify-between gap-3">
            <Label htmlFor="macro-derisk-enabled" className="cursor-pointer">
              De-risk włączony
            </Label>
            <Switch
              id="macro-derisk-enabled"
              checked={enabled}
              onCheckedChange={handleToggle}
              disabled={mutation.isPending}
            />
          </div>
        </CardContent>
      </Card>
    </section>
  );
}
