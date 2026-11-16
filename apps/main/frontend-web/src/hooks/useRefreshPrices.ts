import { createUseRefreshPrices } from "@common/app-core";
import { refreshPrices } from "../api/index.js";

export const useRefreshPrices = createUseRefreshPrices(refreshPrices);
