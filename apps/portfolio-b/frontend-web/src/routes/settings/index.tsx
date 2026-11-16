import { useQuery, useQueryClient } from "@tanstack/react-query";
import { listSettings, updateSetting, type Setting } from "../../api/index.js";

import { Separator } from "@/components/ui/separator";
import { PageHeader } from "@common/ui-primitives/PageHeader";
import { SingleSettingSection } from "@common/ui-primitives/SingleSettingSection";
import { queryKeys } from "@/lib/queryKeys";

export default function SettingsPage() {
  const queryClient = useQueryClient();
  const { data: settingsData, isPending, isError } = useQuery({
    queryKey: queryKeys.settings.all,
    queryFn: listSettings,
    staleTime: 10 * 60_000,
  });

  const settings: Setting[] = settingsData?.data ?? [];

  return (
    <div className="p-6 space-y-8 max-w-2xl">
      <PageHeader title="Ustawienia" />

      <Separator />

      {isPending && <p className="text-muted-foreground text-sm">Ładowanie ustawień...</p>}
      {isError && <p role="alert" className="text-destructive text-sm">Błąd ładowania ustawień.</p>}

      {!isPending && !isError && (
        <SingleSettingSection
          settings={settings}
          settingKey="monthly_contribution_pln"
          title="Miesięczna składka"
          label="Składka miesięczna (PLN)"
          inputId="monthly-amount"
          labelWidthClass="w-40"
          successMessage="Składka miesięczna zaktualizowana"
          errorMessage="Błąd zapisu składki miesięcznej"
          updateSetting={updateSetting}
          onSaved={() => {
            void queryClient.invalidateQueries({ queryKey: queryKeys.settings.all });
          }}
        />
      )}
    </div>
  );
}
