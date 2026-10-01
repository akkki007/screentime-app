/**
 * The hostname of a page, or undefined for anything that shouldn't be tracked
 * (browser-internal pages, files, extensions). Only the hostname ever leaves
 * the browser: never the path, query string or title.
 */
export function domainOf(url: string | undefined): string | undefined {
  if (!url) return undefined;
  try {
    const parsed = new URL(url);
    if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') return undefined;
    return parsed.hostname.toLowerCase() || undefined;
  } catch {
    return undefined;
  }
}
