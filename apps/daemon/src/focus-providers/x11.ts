import type { FocusProvider, FocusedWindow, Unsubscribe } from '@screentime/shared';
import { buildWmClassIndex } from '../desktop-entries';
import { type Runner, bunRunner } from '../runner';
import { parseActiveWindow, parseWindowProps } from '../x11-parse';

const IDLE_POLL_MS = 5_000;

/**
 * X11 (including GNOME on Xorg): focus from `_NET_ACTIVE_WINDOW` via `xprop`,
 * idle from the XScreenSaver extension via `xprintidle`. Uses command-line
 * tools rather than an X client library to keep the daemon pure TypeScript.
 *
 * Without `xprintidle` there is no idle detection, so away time would be
 * counted; the daemon says so at startup.
 */
export class X11FocusProvider implements FocusProvider {
  readonly id = 'x11';
  private wmClassIndex: Map<string, string> | undefined;

  constructor(
    private readonly runner: Runner = bunRunner,
    private readonly env: NodeJS.ProcessEnv = process.env,
    private readonly now: () => number = Date.now,
    private readonly buildIndex: () => Map<string, string> = buildWmClassIndex,
  ) {}

  async isAvailable(): Promise<boolean> {
    if (this.env.XDG_SESSION_TYPE !== 'x11' || !this.env.DISPLAY) return false;
    return this.runner.has('xprop');
  }

  onFocusChange(cb: (w: FocusedWindow) => void): Unsubscribe {
    let disposed = false;
    // xprop calls are async; chaining keeps events in the order they happened.
    let chain: Promise<void> = Promise.resolve();

    const handle = (line: string) => {
      chain = chain.then(async () => {
        if (disposed) return;
        const id = parseActiveWindow(line);
        if (!id) return;
        const window = await this.describe(id);
        if (window && !disposed) cb(window);
      });
    };

    // The spy only reports changes, so fetch the current window once up front.
    void this.runner
      .run(['xprop', '-root', '_NET_ACTIVE_WINDOW'])
      .then(({ stdout }) => handle(stdout));
    const stop = this.runner.stream(['xprop', '-root', '-spy', '_NET_ACTIVE_WINDOW'], handle);

    return () => {
      disposed = true;
      stop.stop();
    };
  }

  onIdleChange(cb: (idle: boolean) => void, thresholdMs: number): Unsubscribe {
    if (!this.runner.has('xprintidle')) {
      console.warn(
        '[x11] xprintidle is not installed, so idle time cannot be detected and time away will be counted. Install it with: sudo apt install xprintidle',
      );
      return () => {};
    }

    let idle = false;
    let disposed = false;
    const poll = async () => {
      const { stdout, ok } = await this.runner.run(['xprintidle']);
      const idleMs = Number.parseInt(stdout.trim(), 10);
      if (disposed || !ok || Number.isNaN(idleMs)) return;
      const nowIdle = idleMs >= thresholdMs;
      if (nowIdle !== idle) {
        idle = nowIdle;
        cb(idle);
      }
    };

    const timer = setInterval(() => void poll(), IDLE_POLL_MS);
    return () => {
      disposed = true;
      clearInterval(timer);
    };
  }

  private async describe(windowId: string): Promise<FocusedWindow | undefined> {
    const { stdout, ok } = await this.runner.run([
      'xprop',
      '-id',
      windowId,
      'WM_CLASS',
      '_NET_WM_PID',
      '_NET_WM_NAME',
    ]);
    if (!ok) return undefined;

    const props = parseWindowProps(stdout);
    if (!props.wmClass) return undefined;

    return {
      appId: this.appIdFor(props.wmClass),
      title: props.title,
      pid: props.pid,
      ts: this.now(),
    };
  }

  /** Prefer the desktop-entry ID (as Wayland reports); fall back to the WM_CLASS itself. */
  private appIdFor(wmClass: string): string {
    this.wmClassIndex ??= this.buildIndex();
    return this.wmClassIndex.get(wmClass.toLowerCase()) ?? wmClass;
  }
}
