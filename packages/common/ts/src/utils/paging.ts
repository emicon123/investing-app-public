import type { PageInfo } from "../schemas/envelope.js";

/**
 * Fetches every page of a paged endpoint and concatenates the rows, so a caller
 * never silently truncates at one page (the history chart outgrew `size: 1000`
 * once MAIN's history reached back to 2021 — 1 874 days).
 */
export async function fetchAllPages<T>(
  fetchPage: (page: number, size: number) => Promise<{ data: T[]; page: PageInfo }>,
  pageSize = 1000,
): Promise<{ data: T[] }> {
  const data: T[] = [];
  for (let page = 0; ; page++) {
    const r = await fetchPage(page, pageSize);
    data.push(...r.data);
    if (r.data.length < pageSize || data.length >= r.page.total) return { data };
  }
}
