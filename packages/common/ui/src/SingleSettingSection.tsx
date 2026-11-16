import { useState } from "react";
import type { ReactNode } from "react";
import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "./button.js";
import { DecimalInput } from "./decimal-input.js";
import { Card, CardContent } from "./card.js";

/**
 * portfolioB/portfolioC only — main's settings page is cash+macro and untouched.
 * Owns findSetting + init-once state + mutation + card chrome. Each app's
 * `routes/settings/index.tsx` keeps its shell (PageHeader, Separator,
 * loading/error states, the `listSettings` query) and renders this with its
 * own copy/config.
 */
export interface SettingLike {
  key: string;
  value: string;
}

export interface SingleSettingSectionProps {
  settings: SettingLike[];
  settingKey: string;
  title: string;
  label: string;
  inputId: string;
  description?: ReactNode;
  /** `w-40` (portfolioB) / `w-56` (portfolioC) — the only class diff. */
  labelWidthClass?: string;
  successMessage: string;
  errorMessage: string;
  updateSetting: (key: string, body: { value: string }) => Promise<unknown>;
  /** Adapter invalidates its own `settings.all` key — keys stay per-app. */
  onSaved: () => void;
}

function findSetting(settings: SettingLike[], key: string): SettingLike | undefined {
  return settings.find((s) => s.key === key);
}

export function SingleSettingSection({
  settings,
  settingKey,
  title,
  label,
  inputId,
  description,
  labelWidthClass,
  successMessage,
  errorMessage,
  updateSetting,
  onSaved,
}: SingleSettingSectionProps) {
  const current = findSetting(settings, settingKey);

  const [amount, setAmount] = useState<string>(current?.value ?? "");

  const mutation = useMutation({
    mutationFn: () => updateSetting(settingKey, { value: amount }),
    onSuccess: () => {
      onSaved();
      toast.success(successMessage);
    },
    onError: () => {
      toast.error(errorMessage);
    },
  });

  return (
    <section className="space-y-4">
      <h2 className="text-lg font-semibold">{title}</h2>
      {description != null && (
        <p className="text-sm text-muted-foreground">{description}</p>
      )}

      <Card>
        <CardContent className="pt-6 space-y-4">
          <div className="flex items-center gap-3">
            <label htmlFor={inputId} className={`text-sm ${labelWidthClass ?? ""} shrink-0`}>
              {label}
            </label>
            <DecimalInput
              id={inputId}
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              className="w-44"
            />
            <span className="text-sm text-muted-foreground">PLN</span>
          </div>
          <Button onClick={() => mutation.mutate()} disabled={mutation.isPending}>
            {mutation.isPending ? "Zapisywanie..." : "Zapisz"}
          </Button>
        </CardContent>
      </Card>
    </section>
  );
}
