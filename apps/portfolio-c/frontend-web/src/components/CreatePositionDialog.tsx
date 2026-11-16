import {
  CreatePositionDialog as SharedCreatePositionDialog,
  type CreatePositionRequestLike,
} from "@common/ui-primitives/CreatePositionDialog";
import { createPosition, type CreatePositionRequest, type Asset } from "../api/index.js";
import { usePortfolio } from "@/lib/portfolio";
import { PORTFOLIO_LABELS } from "@/lib/formConstants";

type Props = {
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
  /** Existing assets of the current portfolio — the attach-to-asset picker's options. */
  assets: Asset[];
};

export function CreatePositionDialog(props: Props) {
  const { portfolio } = usePortfolio();

  function handleCreatePosition(body: CreatePositionRequestLike) {
    return createPosition({ ...body, portfolio } satisfies CreatePositionRequest);
  }

  return (
    <SharedCreatePositionDialog
      {...props}
      createPosition={handleCreatePosition}
      portfolio={portfolio}
      titleSuffix={PORTFOLIO_LABELS[portfolio]}
    />
  );
}
