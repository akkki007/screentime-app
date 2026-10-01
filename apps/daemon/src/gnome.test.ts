import { describe, expect, test } from 'bun:test';
import type { FocusedWindow } from '@screentime/shared';
import { GnomeWaylandFocusProvider } from './focus-providers/gnome-wayland';
import { parseTuple, quoteString } from './gvariant';
import { DbusNotifier } from './notifier';
import type { Runner } from './runner';

describe('parseTuple', () => {
  test('reads strings and integers', () => {
    expect(parseTuple("('org.gnome.Ptyxis', 'Terminal', uint32 4242)")).toEqual([
      'org.gnome.Ptyxis',
      'Terminal',
      4242,
    ]);
    expect(parseTuple('(uint64 749,)')).toEqual([749]);
    expect(parseTuple("('', '', uint32 0)")).toEqual(['', '', 0]);
  });

  test('handles quotes, escapes and unicode in titles', () => {
    expect(parseTuple(`('a', "it's fine", uint32 1)`)).toEqual(['a', "it's fine", 1]);
    expect(parseTuple("('a', 'it\\'s', uint32 1)")).toEqual(['a', "it's", 1]);
    expect(parseTuple("('a', 'x\\\\y\\nz', uint32 1)")).toEqual(['a', 'x\\y\nz', 1]);
    expect(parseTuple("('a', '◑ 日本 — x', uint32 1)")).toEqual(['a', '◑ 日本 — x', 1]);
    // a comma or paren inside a title must not split or end the tuple
    expect(parseTuple("('a', 'x, (y)', uint32 1)")).toEqual(['a', 'x, (y)', 1]);
  });

  test('returns undefined for output it does not understand', () => {
    expect(parseTuple('')).toBeUndefined();
    expect(parseTuple('Error: GDBus.Error:...')).toBeUndefined();
    expect(parseTuple("('unterminated, uint32 1)")).toBeUndefined();
    expect(parseTuple("({'a': 1},)")).toBeUndefined();
  });
});

test('quoteString round-trips through parseTuple', () => {
  for (const s of ['plain', "it's", 'back\\slash', 'line\nbreak', "'; rm -rf / #", '"dq"']) {
    expect(parseTuple(`(${quoteString(s)},)`)).toEqual([s]);
  }
});

function fakeRunner(opts: { has?: boolean; focus?: string; idle?: () => string } = {}) {
  let emit: (line: string) => void = () => {};
  const calls: string[][] = [];
  let stopped = false;
  const runner: Runner = {
    async run(cmd) {
      calls.push(cmd);
      if (cmd.some((c) => c.endsWith('GetFocus')))
        return { stdout: opts.focus ?? '', ok: opts.focus !== undefined };
      if (cmd.some((c) => c.endsWith('GetIdletime')))
        return { stdout: opts.idle?.() ?? '(uint64 0,)', ok: true };
      return { stdout: '', ok: true };
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
    has: () => opts.has ?? true,
  };
  return { runner, calls, emit: (l: string) => emit(l), wasStopped: () => stopped };
}

const wait = (ms = 20) => new Promise((r) => setTimeout(r, ms));
const SIGNAL =
  '/io/github/akkki007/screentime/Focus: io.github.akkki007.screentime.Focus.FocusChanged ';

describe('GnomeWaylandFocusProvider', () => {
  test('is available on GNOME Wayland with gdbus installed', async () => {
    const f = fakeRunner();
    const env = { XDG_SESSION_TYPE: 'wayland', XDG_CURRENT_DESKTOP: 'ubuntu:GNOME' };
    expect(await new GnomeWaylandFocusProvider(f.runner, env).isAvailable()).toBe(true);
    expect(
      await new GnomeWaylandFocusProvider(f.runner, {
        ...env,
        XDG_SESSION_TYPE: 'x11',
      }).isAvailable(),
    ).toBe(false);
    expect(
      await new GnomeWaylandFocusProvider(f.runner, {
        ...env,
        XDG_CURRENT_DESKTOP: 'KDE',
      }).isAvailable(),
    ).toBe(false);
    expect(
      await new GnomeWaylandFocusProvider(fakeRunner({ has: false }).runner, env).isAvailable(),
    ).toBe(false);
  });

  test('reports FocusChanged signals, dropping empty titles and pids', async () => {
    const f = fakeRunner();
    const seen: FocusedWindow[] = [];
    const stop = new GnomeWaylandFocusProvider(f.runner, {}, () => 99).onFocusChange((w) =>
      seen.push(w),
    );
    f.emit(
      'Monitoring signals on object /io/github/akkki007/screentime/Focus owned by io.github.akkki007.screentime',
    );
    f.emit(`${SIGNAL}('org.mozilla.firefox', "It's a page", uint32 7)`);
    f.emit(`${SIGNAL}('org.gnome.Ptyxis', '', uint32 0)`);
    f.emit(`${SIGNAL}('garbage line`);
    expect(seen).toEqual([
      { appId: 'org.mozilla.firefox', title: "It's a page", pid: 7, ts: 99 },
      { appId: 'org.gnome.Ptyxis', title: undefined, pid: undefined, ts: 99 },
    ]);
    stop();
    expect(f.wasStopped()).toBe(true);
    f.emit(`${SIGNAL}('x', '', uint32 1)`);
    expect(seen).toHaveLength(2);
  });

  test('fetches the current window whenever the extension (re)appears', async () => {
    const f = fakeRunner({ focus: "('org.gnome.Ptyxis', 'T', uint32 5)" });
    const seen: FocusedWindow[] = [];
    new GnomeWaylandFocusProvider(f.runner, {}).onFocusChange((w) => seen.push(w));
    f.emit('The name io.github.akkki007.screentime does not have an owner');
    await wait();
    expect(seen).toHaveLength(0);
    f.emit('The name io.github.akkki007.screentime is owned by :1.13');
    await wait();
    expect(seen.map((w) => w.appId)).toEqual(['org.gnome.Ptyxis']);
  });

  test('an extension without GetFocus is tolerated', async () => {
    const f = fakeRunner(); // GetFocus fails
    const seen: FocusedWindow[] = [];
    new GnomeWaylandFocusProvider(f.runner, {}).onFocusChange((w) => seen.push(w));
    f.emit('The name io.github.akkki007.screentime is owned by :1.13');
    await wait();
    expect(seen).toEqual([]);
  });

  test('idle polling reports threshold crossings once each', async () => {
    let idle = '(uint64 0,)';
    const f = fakeRunner({ idle: () => idle });
    const states: boolean[] = [];
    const realSetInterval = globalThis.setInterval;
    let tick: () => void = () => {};
    globalThis.setInterval = ((fn: () => void) => {
      tick = fn;
      return 0 as unknown as ReturnType<typeof setInterval>;
    }) as unknown as typeof setInterval;
    try {
      const stop = new GnomeWaylandFocusProvider(f.runner, {}).onIdleChange(
        (v) => states.push(v),
        180_000,
      );
      for (const ms of [1000, 200000, 400000, 500, 900]) {
        idle = `(uint64 ${ms},)`;
        tick();
        await wait();
      }
      stop();
    } finally {
      globalThis.setInterval = realSetInterval;
    }
    expect(states).toEqual([true, false]);
  });
});

describe('DbusNotifier', () => {
  test('passes user text as safely quoted GVariant arguments', async () => {
    const f = fakeRunner();
    await new DbusNotifier(f.runner).notify("Break's over", 'x\'; rm -rf / "y"');
    const args = f.calls[0] ?? [];
    expect(args.slice(0, 3)).toEqual(['gdbus', 'call', '--session']);
    const tail = args.slice(args.indexOf('Screentime'));
    expect(tail[0]).toBe('Screentime');
    expect(parseTuple(`(${tail[2]},)`)).toEqual(['io.github.akkki007.screentime']);
    expect(parseTuple(`(${tail[3]},)`)).toEqual(["Break's over"]);
    expect(parseTuple(`(${tail[4]},)`)).toEqual(['x\'; rm -rf / "y"']);
  });
});
