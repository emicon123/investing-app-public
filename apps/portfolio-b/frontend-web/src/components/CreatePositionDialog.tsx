import { CreatePositionDialog as SharedCreatePositionDialog } from "@common/ui-primitives/CreatePositionDialog";
import { createPosition, type Asset } from "../api/index.js";

type Props = {
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
  /** Existing assets — the attach-to-asset picker's options. */
  assets: Asset[];
};

export function CreatePositionDialog(props: Props) {
  return <SharedCreatePositionDialog {...props} createPosition={createPosition} />;
}
