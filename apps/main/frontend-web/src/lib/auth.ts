import { createAuthStore } from "@common/app-core";

export const useAuthStore = createAuthStore("auth");
export { extractUserFromBackendJwt } from "@common/app-core";
