import { useQuery } from "@tanstack/react-query";

export function createUsePurchaseHistory<P extends unknown[], T>(
  getPurchaseHistory: (...args: P) => Promise<T>,
  buildKey: (...args: P) => readonly unknown[],
) {
  return (...args: P) =>
    useQuery({
      queryKey: buildKey(...args),
      queryFn: () => getPurchaseHistory(...args),
    });
}
