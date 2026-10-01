# Browser extension

Reports the active tab's **hostname** to the local daemon, so per-site time shows up alongside desktop app usage. Targets Firefox and Chromium-family browsers (Manifest V3).

- `src/background.ts` — the background script. Reports the active tab's domain (never the path, query string or title), ignores non-web pages and **private/incognito windows**, and tells the daemon when you stop looking at a web page.
- `native-host/host.ts` — a Bun program the browser launches over [native messaging](https://developer.chrome.com/docs/extensions/develop/concepts/native-messaging). It decodes the browser's length-prefixed JSON frames (`framing.ts`) and relays them to the daemon's Unix socket as `browser.activeTab` notifications.
- `scripts/build.ts` builds `dist/chromium` and `dist/firefox` (Firefox MV3 uses `background.scripts`, Chromium a `service_worker`) and the host. `scripts/install-native-host.ts` registers the host with your browsers.

Why native messaging and not the socket directly: browsers can't open arbitrary Unix sockets from an extension, so they spawn a small trusted host that can.

The daemon only counts web time while a browser is actually the focused, non-idle app, so leaving a tab open in the background costs nothing.

## Install (development)

```bash
bun run --cwd extensions/browser build

# Firefox
bun run --cwd extensions/browser install:host
#   then about:debugging → This Firefox → Load Temporary Add-on → extensions/browser/dist/firefox/manifest.json

# Chrome / Chromium / Brave: load dist/chromium as an unpacked extension, copy its ID from the
# extensions page, then register the host for that ID:
bun run --cwd extensions/browser install:host -- --browser chrome --chromium-id <32-letter id>

bun run --cwd extensions/browser uninstall:host -- --browser all   # remove again
```

`install:host -- --dry-run` shows what would be written without touching anything.

## Status and known limits

- The native host and its framing are tested, and a framed message was sent through the built host into a running daemon. **The extension has not yet been loaded in a real browser** — treat the browser-side half as unverified.
- Ubuntu's Firefox is a **snap**, and snap confinement blocks native messaging unless the browser can reach the host through the desktop portal. If per-site time stays empty with Firefox from the snap store, that is the likely cause. Mozilla's own `.deb` or tarball builds and Chromium-family browsers installed from `.deb`s do not have this restriction.
- The packaged host is a compiled Bun binary (~80 MB). It is only started while a browser is open.
