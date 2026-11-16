import { apiClient } from "@common/app-core";
import type { TokenExchangeResponse } from "@common/app-core";

export async function exchangeGoogleToken(googleIdToken: string): Promise<TokenExchangeResponse> {
  const response = await apiClient.post<TokenExchangeResponse>("/auth/token", { googleIdToken });
  return response.data;
}
