import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { sendTestPushNotification, type PushDeliveryReportResponse } from "../../../api/index.js";
import { usePushSubscription, type PushState } from "../../../hooks/usePushSubscription.js";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";

const STATUS_TEXT: Record<"denied" | "off" | "busy" | "on", string> = {
  denied: "Powiadomienia są zablokowane w ustawieniach przeglądarki dla tej strony.",
  off: "Powiadomienia są wyłączone na tym urządzeniu.",
  busy: "Trwa zmiana ustawień powiadomień…",
  on: "Powiadomienia są włączone na tym urządzeniu.",
};

const IOS_HINT = "Na iPhonie działa tylko po dodaniu aplikacji do ekranu głównego.";

function showDeliveryReport({ data }: PushDeliveryReportResponse) {
  if (data.attempted === 0) toast.info("Brak zarejestrowanych urządzeń");
  else toast.success(`Wysłano: ${data.delivered}/${data.attempted}`);
}

function SendTestButton() {
  const mutation = useMutation({
    mutationFn: sendTestPushNotification,
    onSuccess: showDeliveryReport,
    onError: () => toast.error("Nie udało się wysłać testowego powiadomienia"),
  });

  return (
    <Button variant="outline" onClick={() => mutation.mutate()} disabled={mutation.isPending}>
      Wyślij testowe powiadomienie
    </Button>
  );
}

function Actions({ state, onEnable, onDisable }: { state: PushState; onEnable: () => void; onDisable: () => void }) {
  switch (state.kind) {
    case "off":
      return <Button onClick={onEnable}>Włącz na tym urządzeniu</Button>;
    case "busy":
      return <Button disabled>Proszę czekać…</Button>;
    case "on":
      return (
        <>
          <Button variant="outline" onClick={onDisable}>
            Wyłącz na tym urządzeniu
          </Button>
          <SendTestButton />
        </>
      );
    default:
      return null;
  }
}

/**
 * Settings card "Powiadomienia" (ADR-108 §9): opts this browser in or out of Web Push and sends a
 * test notification to the owner's own devices. Hidden when the backend has no VAPID keys or the
 * browser lacks the Push API, and while that is still unknown, so it never flashes. It renders its
 * own trailing separator, so a hidden card leaves no stray rule on the page.
 */
export function NotificationsSection() {
  const { state, enable, disable } = usePushSubscription();
  if (state.kind === "hidden" || state.kind === "loading") return null;

  const showIosHint = state.kind === "on" || state.kind === "off";

  return (
    <>
      <section className="space-y-4">
        <h2 className="text-lg font-semibold">Powiadomienia</h2>
        <Card>
          <CardContent className="pt-6 space-y-4">
            <p className="text-sm" aria-live="polite">
              {STATUS_TEXT[state.kind]}
            </p>
            <div className="flex flex-wrap gap-3">
              <Actions state={state} onEnable={enable} onDisable={disable} />
            </div>
            {showIosHint && <p className="text-sm text-muted-foreground">{IOS_HINT}</p>}
          </CardContent>
        </Card>
      </section>
      <Separator />
    </>
  );
}
