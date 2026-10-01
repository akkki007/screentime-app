/**
 * Just enough of GVariant's text format to read `gdbus` output such as
 * `('org.mozilla.firefox', 'Page title', uint32 4242)` and to build safe
 * string arguments for `gdbus call`.
 */

export type GValue = string | number;

const ESCAPES: Record<string, string> = { n: '\n', t: '\t', r: '\r', b: '\b', f: '\f', v: '\v' };

/**
 * Parses a flat tuple of strings and integers. GLib quotes strings with `'`,
 * or with `"` when the text contains a single quote, so both are accepted.
 * Returns undefined for anything it doesn't understand.
 */
export function parseTuple(text: string): GValue[] | undefined {
  const s = text.trim();
  if (!s.startsWith('(') || !s.endsWith(')')) return undefined;

  const out: GValue[] = [];
  let i = 1;
  const end = s.length - 1;

  while (i < end) {
    const c = s[i] as string;
    if (c === ',' || c === ' ') {
      i++;
    } else if (c === "'" || c === '"') {
      let value = '';
      i++;
      for (;;) {
        if (i >= end) return undefined; // unterminated
        const ch = s[i] as string;
        if (ch === c) {
          i++;
          break;
        }
        if (ch === '\\') {
          const next = s[i + 1] as string | undefined;
          if (next === undefined) return undefined;
          value += ESCAPES[next] ?? next;
          i += 2;
        } else {
          value += ch;
          i++;
        }
      }
      out.push(value);
    } else {
      const number = /^(?:(?:u?int(?:16|32|64)|byte|double)\s+)?(-?\d+(?:\.\d+)?)/.exec(
        s.slice(i, end),
      );
      if (!number) return undefined;
      out.push(Number(number[1]));
      i += number[0].length;
    }
  }
  return out;
}

/** A string as a GVariant literal for use as a `gdbus call` argument. */
export function quoteString(value: string): string {
  const escaped = value
    .replaceAll('\\', '\\\\')
    .replaceAll("'", "\\'")
    .replaceAll('\n', '\\n')
    .replaceAll('\r', '\\r')
    .replaceAll('\t', '\\t');
  return `'${escaped}'`;
}
