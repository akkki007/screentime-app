import { afterEach, describe, expect, test } from 'bun:test';
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import type { FocusedWindow } from '@screentime/shared';
import { buildWmClassIndex } from './desktop-entries';
import { X11FocusProvider } from './focus-providers/x11';
import type { Runner } from './runner';
import { parseActiveWindow, parseWindowProps } from './x11-parse';

describe('parseActiveWindow', () => {
  test('extracts the window id', () => {
    expect(parseActiveWindow('_NET_ACTIVE_WINDOW(WINDOW): window id # 0x4a00007')).toBe(
      '0x4a00007',
    );
  });
  test('treats 0x0 and unrelated output as no window', () => {
    expect(parseActiveWindow('_NET_ACTIVE_WINDOW(WINDOW): window id # 0x0')).toBeUndefined();
    expect(parseActiveWindow('_NET_ACTIVE_WINDOW:  not found.')).toBeUndefined();
    expect(parseActiveWindow('')).toBeUndefined();
  });
});

describe('parseWindowProps', () => {
  test('reads class, pid and title', () => {
    const out = `WM_CLASS(STRING) = "Navigator", "firefox"
_NET_WM_PID(CARDINAL) = 12345
_NET_WM_NAME(UTF8_STRING) = "Docs \\"quoted\\" - Mozilla Firefox"
`;
    expect(parseWindowProps(out)).toEqual({
      wmClass: 'firefox',
      pid: 12345,
      title: 'Docs "quoted" - Mozilla Firefox',
    });
  });

  test('copes with missing properties', () => {
    expect(parseWindowProps('WM_CLASS:  not found.\n_NET_WM_PID:  not found.')).toEqual({});
    expect(parseWindowProps('WM_CLASS(STRING) = "xterm", "XTerm"')).toEqual({ wmClass: 'XTerm' });
  });
});

const dirs: string[] = [];
afterEach(() => {
  for (const d of dirs.splice(0)) rmSync(d, { recursive: true, force: true });
});

test('buildWmClassIndex maps StartupWMClass to the desktop-entry id, first directory winning', () => {
  const a = mkdtempSync(join(tmpdir(), 'wm-a-'));
  const b = mkdtempSync(join(tmpdir(), 'wm-b-'));
  dirs.push(a, b);
  writeFileSync(
    join(a, 'org.mozilla.firefox.desktop'),
    '[Desktop Entry]\nName=Firefox\nStartupWMClass=firefox\n',
  );
  writeFileSync(
    join(b, 'firefox-esr.desktop'),
    '[Desktop Entry]\nName=Other\nStartupWMClass=Firefox\n',
  );
  writeFileSync(join(b, 'plain.desktop'), '[Desktop Entry]\nName=No class\n');

  const index = buildWmClassIndex([a, b, '/definitely/missing']);
  expect(index.get('firefox')).toBe('org.mozilla.firefox');
  expect(index.has('plain')).toBe(false);
});

/** A scripted stand-in for `xprop`/`xprintidle`. */
function fakeRunner(opts: {
  windows: Record<string, string>;
  initial?: string;
  has?: string[];
  idleMs?: () => string;
}) {
  let emit: (line: string) => void = () => {};
  let stopped = false;
  const runner: Runner = {
    async run(cmd) {
      if (cmd[0] === 'xprintidle') return { stdout: opts.idleMs?.() ?? '0', ok: true };
      if (cmd.includes('-id')) {
        const out = opts.windows[cmd[cmd.indexOf('-id') + 1] ?? ''];
        return { stdout: out ?? '', ok: out !== undefined };
      }
      return { stdout: opts.initial ?? '', ok: true };
    },
    stream(_cmd, onLine) {
      emit = onLine;
      return {
        stop: () => {
          stopped = true;
        },
        exited: new Promise<void>(() => {}),
      };
    },
    has: (b) => (opts.has ?? ['xprop', 'xprintidle']).includes(b),
  };
  return { runner, emit: (line: string) => emit(line), wasStopped: () => stopped };
}

const firefox =
  'WM_CLASS(STRING) = "Navigator", "firefox"\n_NET_WM_PID(CARDINAL) = 7\n_NET_WM_NAME(UTF8_STRING) = "Home"\n';
const term =
  'WM_CLASS(STRING) = "gnome-terminal-server", "Gnome-terminal"\n_NET_WM_PID(CARDINAL) = 9\n';
const wait = (ms = 20) => new Promise((r) => setTimeout(r, ms));

describe('X11FocusProvider', () => {
  const index = () => new Map([['firefox', 'org.mozilla.firefox']]);

  test('is available only on an X11 session with xprop installed', async () => {
    const f = fakeRunner({ windows: {} });
    expect(
      await new X11FocusProvider(f.runner, {
        XDG_SESSION_TYPE: 'x11',
        DISPLAY: ':0',
      }).isAvailable(),
    ).toBe(true);
    expect(
      await new X11FocusProvider(f.runner, {
        XDG_SESSION_TYPE: 'wayland',
        DISPLAY: ':0',
      }).isAvailable(),
    ).toBe(false);
    expect(await new X11FocusProvider(f.runner, { XDG_SESSION_TYPE: 'x11' }).isAvailable()).toBe(
      false,
    );
    const noXprop = fakeRunner({ windows: {}, has: [] });
    expect(
      await new X11FocusProvider(noXprop.runner, {
        XDG_SESSION_TYPE: 'x11',
        DISPLAY: ':0',
      }).isAvailable(),
    ).toBe(false);
  });

  test('reports the current window at start, then each change in order', async () => {
    const f = fakeRunner({
      windows: { '0x1': firefox, '0x2': term },
      initial: '_NET_ACTIVE_WINDOW(WINDOW): window id # 0x1',
    });
    const provider = new X11FocusProvider(f.runner, {}, () => 1234, index);
    const seen: FocusedWindow[] = [];
    const stop = provider.onFocusChange((w) => seen.push(w));
    await wait();

    f.emit('_NET_ACTIVE_WINDOW(WINDOW): window id # 0x2');
    f.emit('_NET_ACTIVE_WINDOW(WINDOW): window id # 0x0'); // nothing focused: ignored
    f.emit('_NET_ACTIVE_WINDOW(WINDOW): window id # 0x1');
    await wait(40);

    expect(seen.map((w) => w.appId)).toEqual([
      'org.mozilla.firefox',
      'Gnome-terminal',
      'org.mozilla.firefox',
    ]);
    expect(seen[0]).toEqual({ appId: 'org.mozilla.firefox', title: 'Home', pid: 7, ts: 1234 });

    stop();
    expect(f.wasStopped()).toBe(true);
    f.emit('_NET_ACTIVE_WINDOW(WINDOW): window id # 0x2');
    await wait();
    expect(seen).toHaveLength(3); // nothing after unsubscribe
  });

  test('skips windows xprop cannot describe', async () => {
    const f = fakeRunner({ windows: {} });
    const seen: FocusedWindow[] = [];
    new X11FocusProvider(f.runner, {}, Date.now, index).onFocusChange((w) => seen.push(w));
    f.emit('_NET_ACTIVE_WINDOW(WINDOW): window id # 0x99');
    await wait();
    expect(seen).toEqual([]);
  });

  test('idle polling reports transitions across the threshold only', async () => {
    let idle = '0';
    const f = fakeRunner({ windows: {}, idleMs: () => idle });
    const provider = new X11FocusProvider(f.runner, {});
    const states: boolean[] = [];

    const realSetInterval = globalThis.setInterval;
    let tick: () => void = () => {};
    globalThis.setInterval = ((fn: () => void) => {
      tick = fn;
      return 0 as unknown as ReturnType<typeof setInterval>;
    }) as unknown as typeof setInterval;
    try {
      const stop = provider.onIdleChange((v) => states.push(v), 180_000);
      for (const ms of ['1000', '200000', '300000', '500', '700']) {
        idle = ms;
        tick();
        await wait();
      }
      stop();
    } finally {
      globalThis.setInterval = realSetInterval;
    }
    expect(states).toEqual([true, false]);
  });

  test('without xprintidle, idle detection is off rather than an error', () => {
    const f = fakeRunner({ windows: {}, has: ['xprop'] });
    const stop = new X11FocusProvider(f.runner, {}).onIdleChange(() => {}, 180_000);
    expect(typeof stop).toBe('function');
    stop();
  });
});
