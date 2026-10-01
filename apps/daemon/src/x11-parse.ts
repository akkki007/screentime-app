/** Parsers for `xprop` output. Pure, so they can be tested without an X server. */

/** The window ID from a `_NET_ACTIVE_WINDOW` line, or undefined when no window is active. */
export function parseActiveWindow(line: string): string | undefined {
  const match = /_NET_ACTIVE_WINDOW\(WINDOW\):.*#\s*(0x[0-9a-fA-F]+)/.exec(line);
  const id = match?.[1];
  if (!id) return undefined;
  return Number.parseInt(id, 16) === 0 ? undefined : id;
}

export type WindowProps = { wmClass?: string; pid?: number; title?: string };

/**
 * Reads WM_CLASS, _NET_WM_PID and _NET_WM_NAME from `xprop` output. WM_CLASS is
 * `"instance", "Class"`; the class (second string) is what identifies the app.
 */
export function parseWindowProps(text: string): WindowProps {
  const props: WindowProps = {};

  const cls = /^WM_CLASS\(STRING\)\s*=\s*"((?:[^"\\]|\\.)*)"\s*,\s*"((?:[^"\\]|\\.)*)"/m.exec(text);
  if (cls?.[2]) props.wmClass = unescapeXprop(cls[2]);
  else if (cls?.[1]) props.wmClass = unescapeXprop(cls[1]);

  const pid = /^_NET_WM_PID\(CARDINAL\)\s*=\s*(\d+)/m.exec(text);
  if (pid?.[1]) props.pid = Number(pid[1]);

  const name = /^_NET_WM_NAME\(UTF8_STRING\)\s*=\s*"((?:[^"\\]|\\.)*)"/m.exec(text);
  if (name?.[1] !== undefined) props.title = unescapeXprop(name[1]);

  return props;
}

function unescapeXprop(value: string): string {
  return value.replace(/\\(["\\])/g, '$1');
}
