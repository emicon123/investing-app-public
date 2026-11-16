import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { deletePushSubscription, getPushConfig, upsertPushSubscription } from "../api/index.js";
import { disable, enable, isPushSupported, readPermission, restore, type EnableResult } from "../lib/push.js";
import { queryKeys } from "../lib/queryKeys.js";

/**
 * UI state of this browser's Web Push subscription (design doc 172-web-push §5). A discriminated
 * union rather than GoF State classes: there is no per-state behaviour beyond what the card renders.
 */
export type PushState =
  | { kind: "loading" }
  | { kind: "hidden" }
  | { kind: "denied" }
  | { kind: "off" }
  | { kind: "busy" }
  | { kind: "on" };

type DeviceState = "denied" | "off" | "on";

export interface PushSubscriptionControls {
  state: PushState;
  /** Off → Busy → On / Denied / Off. */
  enable: () => void;
  /** On → Busy → Off. */
  disable: () => void;
}

const ENABLE_ERROR = "Nie udało się włączyć powiadomień";
const DISABLE_ERROR = "Nie udało się wyłączyć powiadomień";

/**
 * Mount-time sync while the card is visible: blocked → denied; no subscription → off; otherwise
 * resubscribe when the VAPID key was rotated and re-POST (an idempotent upsert that restores a
 * row the backend pruned). Failing to re-POST leaves the browser subscribed, so it is still `on`.
 */
async function syncDevice(vapidPublicKey: string): Promise<DeviceState> {
  if (readPermission() === "denied") return "denied";
  let restored;
  try {
    restored = await restore(vapidPublicKey);
  } catch {
    return "off";
  }
  if (!restored) return "off";
  await Promise.allSettled([
    upsertPushSubscription(restored.subscription),
    restored.replacedEndpoint ? deletePushSubscription(restored.replacedEndpoint) : Promise.resolve(),
  ]);
  return "on";
}

async function enableDevice(vapidPublicKey: string): Promise<EnableResult["status"]> {
  const result = await enable(vapidPublicKey);
  if (result.status === "subscribed") await upsertPushSubscription(result.subscription);
  return result.status;
}

async function disableDevice(): Promise<void> {
  const endpoint = await disable();
  if (endpoint) await deletePushSubscription(endpoint);
}

const DEVICE_STATE_AFTER_ENABLE: Record<EnableResult["status"], DeviceState> = {
  subscribed: "on",
  denied: "denied",
  dismissed: "off",
};

export function usePushSubscription(): PushSubscriptionControls {
  const queryClient = useQueryClient();
  const supported = isPushSupported();

  const config = useQuery({
    queryKey: queryKeys.push.config,
    queryFn: getPushConfig,
    enabled: supported,
    staleTime: 10 * 60_000,
  });
  const vapidPublicKey = config.data?.data.enabled ? config.data.data.vapidPublicKey : undefined;

  const device = useQuery({
    queryKey: queryKeys.push.device,
    queryFn: () => syncDevice(vapidPublicKey ?? ""),
    enabled: vapidPublicKey !== undefined,
    staleTime: Infinity,
    gcTime: 0,
  });

  const setDevice = (next: DeviceState) => queryClient.setQueryData(queryKeys.push.device, next);

  const enableMutation = useMutation({
    mutationFn: () => enableDevice(vapidPublicKey ?? ""),
    onSuccess: (status) => setDevice(DEVICE_STATE_AFTER_ENABLE[status]),
    onError: () => {
      setDevice("off");
      toast.error(ENABLE_ERROR);
    },
  });

  const disableMutation = useMutation({
    mutationFn: disableDevice,
    onSuccess: () => setDevice("off"),
    onError: () => {
      setDevice("off");
      toast.error(DISABLE_ERROR);
    },
  });

  return {
    state: deriveState({
      supported,
      configPending: config.isPending,
      configured: vapidPublicKey !== undefined,
      busy: enableMutation.isPending || disableMutation.isPending,
      device: device.data,
    }),
    enable: () => enableMutation.mutate(),
    disable: () => disableMutation.mutate(),
  };
}

function deriveState(s: {
  supported: boolean;
  configPending: boolean;
  configured: boolean;
  busy: boolean;
  device: DeviceState | undefined;
}): PushState {
  if (!s.supported) return { kind: "hidden" };
  if (s.configPending) return { kind: "loading" };
  if (!s.configured) return { kind: "hidden" };
  if (s.busy) return { kind: "busy" };
  if (!s.device) return { kind: "loading" };
  return { kind: s.device };
}
