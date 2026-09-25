# Browser extension

Reports the active tab's domain and time to the local daemon, so per-site usage shows up alongside desktop app usage. **Status: Phase 4** in the roadmap — this is an early scaffold, not yet functional end-to-end.

- `src/background.ts` — MV3 service worker; tracks the active tab's domain and forwards it to the native messaging host. Targets both Firefox and Chromium (`manifest.json` includes `browser_specific_settings.gecko`).
- `native-host/host.ts` — a Bun script the browser launches as a subprocess over [native messaging](https://developer.chrome.com/docs/extensions/develop/concepts/native-messaging). Relays `{domain, active}` from stdin to the daemon's Unix socket as a `browser.activeTab` notification.

## Why native messaging, not the socket directly

Browsers can't open arbitrary Unix sockets from an extension. Native messaging lets the browser spawn a small trusted host process (built with the same Bun/TypeScript toolchain as everything else) that can.

## Development

```bash
bun run --cwd extensions/browser build
```

Then load `extensions/browser` as an unpacked/temporary extension in your browser, and register `dist/native-host/host.ts`'s compiled output as a native messaging host per the browser's docs (a native messaging host manifest JSON pointing at the built script, keyed by `io.github.akkki007.screentime`).
