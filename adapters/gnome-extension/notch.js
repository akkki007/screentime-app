/**
 * What the notch shows (docs/adr/0014-top-of-screen-notch.md). A port of
 * internal/notch for the GNOME Shell: both pass internal/notch/testdata/
 * fixtures.json, so the Shell's notch and every other desktop's agree. Pure
 * JavaScript, no GNOME imports, so Bun can test it.
 */

/** How long a temporary view stays, in ms. */
export const ALERT_MS = 6000;
export const REMINDER_MS = 6000;
export const GREETING_MS = 5000;
const MAX_DETAIL = 60;
export const PANEL_APPS = 4;

const pad = (n) => String(n).padStart(2, '0');

/** The dashboard's compact duration: <1m, 45m, 3h, 3h 17m. */
export function formatShort(ms) {
  const minutes = Math.floor((ms + 30000) / 60000);
  if (minutes < 1) return ms > 0 ? '<1m' : '0m';
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  if (h === 0) return `${m}m`;
  return m === 0 ? `${h}h` : `${h}h ${m}m`;
}

/** The time left as 24:13, or 1:05:00 from an hour. */
export function countdown(ms) {
  const s = Math.ceil(Math.max(0, ms) / 1000);
  const h = Math.floor(s / 3600);
  const m = Math.floor(s / 60) % 60;
  const sec = s % 60;
  return h > 0 ? `${h}:${pad(m)}:${pad(sec)}` : `${pad(m)}:${pad(sec)}`;
}

function clip(s, n) {
  const t = s.trim();
  const chars = Array.from(t);
  if (chars.length <= n) return t;
  return `${chars
    .slice(0, n - 1)
    .join('')
    .replace(/ +$/, '')}…`;
}

const view = (kind, icon, text, detail = '', countdownTo = null) => ({
  kind,
  icon,
  text,
  detail,
  countdownTo,
});

export class NotchModel {
  /** @param {{userName?: string, tzOffsetMin?: number}} opts */
  constructor(opts = {}) {
    this.opts = { userName: '', tzOffsetMin: 0, ...opts };
    this.connected = false;
    this.status = null;
    this.apps = [];
    this.total = 0;
    this.temp = null;
    this.greeted = '';
  }

  setOptions(opts) {
    this.opts = { ...this.opts, ...opts };
  }

  setConnected(c) {
    this.connected = c;
    if (!c) this.status = null;
  }

  /** The tracker's status; greets at the first activity of a local day. */
  setStatus(s, at) {
    this.status = s;
    this.connected = true;
    const active = Boolean(s.currentAppId) && !s.idle && !s.paused;
    const day = this._localDay(at);
    if (active && day !== this.greeted) {
      this.greeted = day;
      this._show(view('greeting', 'wave', this._greeting(at)), 3, at + GREETING_MS, at);
    }
  }

  /** Today's per-app totals: [{appId, name, ms}]. */
  setToday(apps) {
    this.apps = [...apps].sort((a, b) => b.ms - a.ms);
    this.total = apps.reduce((sum, a) => sum + a.ms, 0);
  }

  limitHit(target, dailyMs, at) {
    this._show(
      view('alert', 'limit', 'Daily limit reached', `${target} · ${formatShort(dailyMs)}`),
      1,
      at + ALERT_MS,
      at,
    );
  }

  reminder(kind, message, at) {
    const [icon, text] =
      kind === 'break'
        ? ['break', 'Time for a break']
        : kind === 'downtime'
          ? ['moon', 'Downtime']
          : ['bolt', 'Stay focused'];
    this._show(view('reminder', icon, text, clip(message, MAX_DETAIL)), 2, at + REMINDER_MS, at);
  }

  _show(v, priority, until, at) {
    const t = this.temp;
    if (t && at < t.until && t.priority < priority) return;
    this.temp = { view: v, priority, until };
  }

  /** What to show at `now`. */
  view(now) {
    if (this.temp) {
      if (now < this.temp.until) return this.temp.view;
      this.temp = null;
    }
    const s = this.status;
    if (!this.connected) return view('offline', 'offline', 'Screentime is not running');
    if (s?.focusMode?.active && s.focusMode.until != null) {
      const until = s.focusMode.until;
      return view('focus', 'timer', countdown(until - now), 'Focus', until);
    }
    if (s?.paused) {
      return view(
        'paused',
        'pause',
        'Paused',
        s.resumeAt != null ? `until ${this._clock(s.resumeAt)}` : '',
      );
    }
    return view('idle', 'clock', formatShort(this.total));
  }

  /** When the view would change with no new event, or 0 for never. */
  nextChange(now) {
    let next = 0;
    if (this.temp && now < this.temp.until) next = this.temp.until;
    const f = this.status?.focusMode;
    if (this.connected && f?.active && f.until != null && now < f.until) {
      let step = (f.until - now) % 1000;
      if (step === 0) step = 1000;
      const tick = now + step;
      if (next === 0 || tick < next) next = tick;
    }
    return next;
  }

  /** The expanded panel. */
  panel() {
    const total = this.total;
    return {
      total: formatShort(total),
      paused: Boolean(this.status?.paused),
      focus: Boolean(this.status?.focusMode?.active),
      apps: this.apps.slice(0, PANEL_APPS).map((a) => ({
        name: a.name || a.appId,
        time: formatShort(a.ms),
        percent: total > 0 ? Math.floor((a.ms * 100 + Math.floor(total / 2)) / total) : 0,
      })),
    };
  }

  _local(at) {
    return new Date(at + this.opts.tzOffsetMin * 60000);
  }

  _localDay(at) {
    const d = this._local(at);
    return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`;
  }

  _clock(at) {
    const d = this._local(at);
    return `${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}`;
  }

  _greeting(at) {
    const h = this._local(at).getUTCHours();
    const part = h < 12 ? 'morning' : h < 18 ? 'afternoon' : 'evening';
    const first = this.opts.userName.trim().split(/\s+/)[0];
    return first ? `Good ${part}, ${first}!` : `Good ${part}!`;
  }
}
