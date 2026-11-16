import type { Asset, Subasset } from "../api/index.js";
import type { DeleteTarget } from "@/hooks/useAssetActions";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { SOFT_DELETE_CONSEQUENCE } from "@common/ui-primitives/portfolio/model/deleteCopy";
import { getAssetIconUrl } from "@/lib/assetIcons";

/**
 * Dialogs of the Portfolio and Retirement pages' asset actions (task 156) — the instrument picker
 * an asset row's pencil opens when the asset has several subassets, and the delete confirm (a whole
 * asset with its subassets, or one subasset). State and mutations live in `useAssetActions`.
 */

export function SubassetPickerDialog({
  asset,
  onPick,
  onClose,
}: {
  asset: Asset | null;
  onPick: (asset: Asset, sub: Subasset) => void;
  onClose: () => void;
}) {
  if (asset === null) return null;
  return (
    <Dialog open onOpenChange={(o) => { if (!o) onClose(); }}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>Wybierz instrument — {asset.name}</DialogTitle>
        </DialogHeader>
        <div className="space-y-2 py-2">
          {asset.subassets.map((sub) => {
            const iconUrl = getAssetIconUrl(sub.ticker, asset.assetClass);
            return (
              <Button
                key={sub.id}
                variant="outline"
                className="w-full justify-start gap-3 h-auto py-3"
                onClick={() => onPick(asset, sub)}
              >
                {iconUrl && (
                  <img
                    src={iconUrl}
                    alt=""
                    className="size-5 rounded-full shrink-0 object-contain"
                    onError={(e) => { (e.currentTarget as HTMLImageElement).style.display = "none"; }}
                  />
                )}
                <span className="font-mono text-sm">{sub.ticker}</span>
                <span className="text-muted-foreground text-sm truncate">{sub.name}</span>
                {sub.isPrimary && (
                  <Badge variant="secondary" className="ml-auto px-1 py-0 text-[10px] font-normal">
                    Główna
                  </Badge>
                )}
              </Button>
            );
          })}
        </div>
      </DialogContent>
    </Dialog>
  );
}

export function DeleteAssetDialog({
  target,
  pending,
  errorMessage,
  onConfirm,
  onClose,
}: {
  target: DeleteTarget | null;
  pending: boolean;
  errorMessage: string | null;
  onConfirm: () => void;
  onClose: () => void;
}) {
  return (
    <AlertDialog open={target != null} onOpenChange={(o) => { if (!o) onClose(); }}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{target?.kind === "asset" ? "Usuń aktywo" : "Usuń instrument"}</AlertDialogTitle>
          <AlertDialogDescription>
            Czy na pewno chcesz usunąć <strong>{target?.ticker}</strong> ({target?.name})?
            {target?.note ? ` ${target.note}` : ""} {SOFT_DELETE_CONSEQUENCE}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {errorMessage && (
          <p role="alert" className="text-sm text-destructive">
            {errorMessage}
          </p>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>Anuluj</AlertDialogCancel>
          <AlertDialogAction
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            disabled={pending}
            onClick={(e) => {
              // Keep the dialog open until the request settles so a 409/404 message can show.
              e.preventDefault();
              onConfirm();
            }}
          >
            {pending ? "Usuwanie..." : "Usuń"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
