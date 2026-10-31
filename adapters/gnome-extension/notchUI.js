/**
 * The notch in the GNOME Shell (docs/adr/0014-top-of-screen-notch.md): a black
 * pill that hangs from the bottom of the top bar, centred. What it shows is
 * decided by NotchModel (notch.js); this file only draws it and feeds it from
 * the daemon. With nothing happening it does no work: it redraws when the
 * daemon says something, when a temporary view ends, once a second during a
 * focus session, and once a minute to refresh today's total.
 */
import Clutter from 'gi://Clutter';
import GLib from 'gi://GLib';
import GObject from 'gi://GObject';
import Shell from 'gi://Shell';
import St from 'gi://St';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';

import { DaemonClient } from './daemonClient.js';
import { NotchModel, formatShort } from './notch.js';
import { StatsPanel } from './notchPanel.js';

const INK = [0, 0, 0]; // the top bar is pure black: any other tone shows a seam where they join
const SHOULDER = 18;
const PILL_HEIGHT = 34;
const MIN_WIDTH = 132;
const ANIM_MS = 180;

// Icons are drawn with cairo on a 24 x 24 grid; the strokes round off like the app's.
const ICONS = {
  clock: (cr) => {
    cr.arc(12, 12, 8.5, 0, 2 * Math.PI);
    cr.stroke();
    cr.moveTo(12, 7.5);
    cr.lineTo(12, 12);
    cr.lineTo(15, 14);
    cr.stroke();
  },
  timer: (cr) => {
    cr.arc(12, 13.5, 7.5, 0, 2 * Math.PI);
    cr.stroke();
    cr.moveTo(12, 13.5);
    cr.lineTo(12, 9.5);
    cr.moveTo(10, 3);
    cr.lineTo(14, 3);
    cr.stroke();
  },
  pause: (cr) => {
    cr.rectangle(7, 5.5, 3.6, 13);
    cr.rectangle(13.4, 5.5, 3.6, 13);
    cr.fill();
  },
  limit: (cr) => {
    cr.moveTo(7, 3.5);
    cr.lineTo(17, 3.5);
    cr.moveTo(7, 20.5);
    cr.lineTo(17, 20.5);
    cr.moveTo(8, 3.5);
    cr.curveTo(8, 10, 16, 10, 16, 3.5);
    cr.moveTo(8, 20.5);
    cr.curveTo(8, 14, 16, 14, 16, 20.5);
    cr.stroke();
  },
  break: (cr) => {
    cr.moveTo(5, 10);
    cr.lineTo(16, 10);
    cr.lineTo(16, 14);
    cr.curveTo(16, 17.5, 13.5, 19, 10.5, 19);
    cr.curveTo(7.5, 19, 5, 17.5, 5, 14);
    cr.closePath();
    cr.stroke();
    cr.moveTo(16, 11.5);
    cr.curveTo(19.5, 11.5, 19.5, 16, 16, 16);
    cr.moveTo(8, 3);
    cr.lineTo(8, 6);
    cr.moveTo(11.5, 3);
    cr.lineTo(11.5, 6);
    cr.stroke();
  },
  moon: (cr) => {
    cr.moveTo(20, 14.5);
    cr.curveTo(13, 19, 5, 13, 9.5, 4);
    cr.curveTo(4, 5, 2.5, 13, 8, 18.5);
    cr.curveTo(13, 22, 18.5, 18.5, 20, 14.5);
    cr.fill();
  },
  bolt: (cr) => {
    cr.moveTo(13, 3);
    cr.lineTo(5.5, 13.5);
    cr.lineTo(11, 13.5);
    cr.lineTo(10, 21);
    cr.lineTo(18.5, 10);
    cr.lineTo(13, 10);
    cr.closePath();
    cr.fill();
  },
  wave: (cr) => {
    cr.arc(12, 12, 8.5, 0, 2 * Math.PI);
    cr.stroke();
    cr.arc(9, 10, 1, 0, 2 * Math.PI);
    cr.arc(15, 10, 1, 0, 2 * Math.PI);
    cr.fill();
    cr.moveTo(8, 14.5);
    cr.curveTo(10, 17.5, 14, 17.5, 16, 14.5);
    cr.stroke();
  },
  offline: (cr) => {
    cr.arc(12, 12, 8.5, 0, 2 * Math.PI);
    cr.stroke();
    cr.moveTo(12, 7.5);
    cr.lineTo(12, 13);
    cr.stroke();
    cr.arc(12, 16.2, 0.9, 0, 2 * Math.PI);
    cr.fill();
  },
};

const Icon = GObject.registerClass(
  class ScreentimeNotchIcon extends St.DrawingArea {
    _init(size) {
      super._init({ width: size, height: size, y_align: Clutter.ActorAlign.CENTER });
      this._size = size;
      this._name = 'clock';
    }

    setIcon(name) {
      this._name = ICONS[name] ? name : 'clock';
      this.queue_repaint();
    }

    vfunc_repaint() {
      const cr = this.get_context();
      const s = this._size / 24;
      cr.scale(s, s);
      cr.setSourceRGBA(1, 1, 1, 1);
      cr.setLineWidth(2);
      cr.setLineCap(1); // round
      cr.setLineJoin(1);
      ICONS[this._name](cr);
      cr.$dispose();
    }
  },
);

/** The concave piece that joins the pill to the top bar. `left` mirrors it. */
const Shoulder = GObject.registerClass(
  class ScreentimeNotchShoulder extends St.DrawingArea {
    _init(left) {
      super._init({ width: SHOULDER, height: SHOULDER, y_align: Clutter.ActorAlign.START });
      this._left = left;
    }

    vfunc_repaint() {
      const cr = this.get_context();
      cr.setSourceRGBA(...INK, 1);
      if (this._left) {
        // the filler between the bar and the pill's left edge
        cr.moveTo(0, 0);
        cr.lineTo(SHOULDER, 0);
        cr.lineTo(SHOULDER, SHOULDER);
        cr.arcNegative(0, SHOULDER, SHOULDER, 0, -Math.PI / 2);
      } else {
        cr.moveTo(SHOULDER, 0);
        cr.lineTo(0, 0);
        cr.lineTo(0, SHOULDER);
        cr.arc(SHOULDER, SHOULDER, SHOULDER, Math.PI, 1.5 * Math.PI);
      }
      cr.closePath();
      cr.fill();
      cr.$dispose();
    }
  },
);

export class Notch {
  /** @param {{gettext?: (s: string) => string}} [_opts] */
  constructor(_opts) {
    this._model = new NotchModel({
      userName: GLib.get_real_name() === 'Unknown' ? '' : GLib.get_real_name(),
      tzOffsetMin: -new Date().getTimezoneOffset(),
    });
    // Off until the tracker says otherwise: a notch the user turned off (or
    // never turned on) must never flash at login or while the tracker is down.
    this._enabled = false;
    this._shown = false;
    this._limits = new Map();
    this._names = new Map();
    this._open = false;
    this._redrawId = 0;
    this._minuteId = 0;
    this._client = new DaemonClient({
      onNotification: (method, params) => this._onNotification(method, params),
      onState: (connected) => this._onState(connected),
    });
  }

  enable() {
    this._build();
    this._client.start();
    this._monitorsId = Main.layoutManager.connect('monitors-changed', () => this._place());
    this._redraw();
  }

  disable() {
    this._close();
    this._client.stop();
    for (const id of [this._redrawId, this._minuteId]) if (id) GLib.source_remove(id);
    this._redrawId = this._minuteId = 0;
    if (this._monitorsId) Main.layoutManager.disconnect(this._monitorsId);
    this._monitorsId = 0;
    if (this._root) {
      Main.layoutManager.removeChrome(this._root);
      this._root.destroy();
      this._root = null;
    }
  }

  /** Toggle the panel; also what a click on the pill does. */
  toggle() {
    if (this._open) this._close();
    else this._openPanel();
  }

  // ---- building ---------------------------------------------------------

  _build() {
    this._root = new St.BoxLayout({
      vertical: true,
      reactive: false,
      visible: false,
      x_align: Clutter.ActorAlign.START,
    });
    const row = new St.BoxLayout({
      reactive: false,
      x_align: Clutter.ActorAlign.CENTER,
      y_align: Clutter.ActorAlign.START,
    });
    this._row = row;
    this._left = new Shoulder(true);
    this._right = new Shoulder(false);

    this._pill = new St.BoxLayout({
      reactive: true,
      track_hover: true,
      can_focus: true,
      clip_to_allocation: true,
      height: PILL_HEIGHT,
      y_align: Clutter.ActorAlign.START,
      style: `background-color: rgb(0,0,0); border-radius: 0 0 ${PILL_HEIGHT / 2 + 3}px ${PILL_HEIGHT / 2 + 3}px;`,
    });
    this._content = new St.BoxLayout({
      y_align: Clutter.ActorAlign.CENTER,
      style: 'spacing: 10px; padding: 0 18px 2px 18px;',
    });
    this._icon = new Icon(18);
    this._text = new St.Label({
      y_align: Clutter.ActorAlign.CENTER,
      style: 'color: white; font-weight: 600; font-size: 13px; font-feature-settings: "tnum";',
    });
    this._detail = new St.Label({
      y_align: Clutter.ActorAlign.CENTER,
      style: 'color: rgb(165,155,147); font-size: 12px;',
    });
    this._content.add_child(this._icon);
    this._content.add_child(this._text);
    this._content.add_child(this._detail);
    // During focus mode a round button ends the session, like the timer's pause button in the inspiration.
    this._stopBtn = new St.Button({
      reactive: true,
      can_focus: true,
      track_hover: true,
      visible: false,
      width: 24,
      height: 24,
      y_align: Clutter.ActorAlign.CENTER,
      style: 'border-radius: 12px; background-color: rgb(46,42,39);',
      child: new St.Widget({
        width: 8,
        height: 8,
        x_align: Clutter.ActorAlign.CENTER,
        y_align: Clutter.ActorAlign.CENTER,
        style: 'background-color: white; border-radius: 2px;',
      }),
    });
    this._stopBtn.connect('clicked', () => {
      this._client.call('focus.stop').catch((e) => console.error(`Screentime notch: ${e.message}`));
      return Clutter.EVENT_STOP;
    });
    this._content.add_child(this._stopBtn);
    this._pill.add_child(this._content);

    // Keep it centred while it grows or shrinks.
    this._pill.connect('notify::width', () => this._place());
    this._pill.connect('button-press-event', () => {
      this.toggle();
      return Clutter.EVENT_STOP;
    });

    row.add_child(this._left);
    row.add_child(this._pill);
    row.add_child(this._right);
    this._root.add_child(row);

    this._stats = new StatsPanel({
      onClose: () => this._close(),
      onSaveLimit: (minutes, overlay) => this._saveLimit(minutes, overlay),
      onRemoveLimit: () => this._removeLimit(),
      onTogglePause: () => this._togglePause(),
    });
    this._panel = this._stats.actor;
    this._panel.connect('key-press-event', (_a, ev) => {
      if (ev.get_key_symbol() === Clutter.KEY_Escape) {
        if (this._stats._popover) this._stats.closePopover();
        else this._close();
        return Clutter.EVENT_STOP;
      }
      return Clutter.EVENT_PROPAGATE;
    });
    this._root.add_child(this._panel);

    Main.layoutManager.addTopChrome(this._root, { trackFullscreen: true });
  }

  _place() {
    if (!this._root) return;
    const m = Main.layoutManager.primaryMonitor;
    if (!m) return;
    const [w] = this._root.get_size();
    this._root.set_position(Math.round(m.x + (m.width - w) / 2), m.y + Main.panel.height);
  }

  // ---- drawing ----------------------------------------------------------

  _now() {
    return GLib.get_real_time() / 1000;
  }

  _animate() {
    return St.Settings.get().enable_animations;
  }

  _redraw() {
    if (!this._root) return;
    if (this._redrawId) {
      GLib.source_remove(this._redrawId);
      this._redrawId = 0;
    }
    const now = Math.floor(this._now());
    // "Screentime is not running" only appears once the notch has been shown this
    // session and the tracker then went away; a notch that is off stays gone.
    this._shown = this._shown || (this._enabled && this._client.connected);
    const visible = this._shown && (this._enabled || !this._client.connected);
    this._root.visible = visible;
    if (!visible) {
      this._close();
      return;
    }
    const v = this._model.view(now);
    this._icon.setIcon(v.icon);
    this._text.text = v.text;
    this._detail.text = v.detail;
    this._detail.visible = Boolean(v.detail);
    this._stopBtn.visible = v.kind === 'focus';
    this._setPillWidth();
    if (this._open) this._fillPanel();

    const next = this._model.nextChange(now);
    if (next > 0) {
      this._redrawId = GLib.timeout_add(GLib.PRIORITY_DEFAULT, Math.max(50, next - now), () => {
        this._redrawId = 0;
        this._redraw();
        return GLib.SOURCE_REMOVE;
      });
    }
  }

  _setPillWidth() {
    const [, natural] = this._content.get_preferred_width(-1);
    const target = Math.max(MIN_WIDTH, natural);
    const current = this._pill.width > 0 ? this._pill.width : target;
    this._pill.remove_all_transitions();
    if (!this._animate() || current === target) {
      this._pill.set_width(target);
      this._place();
      return;
    }
    this._pill.set_width(current);
    this._pill.ease({
      width: target,
      duration: ANIM_MS,
      mode: Clutter.AnimationMode.EASE_OUT_QUAD,
      onComplete: () => this._place(),
    });
  }

  // ---- the panel --------------------------------------------------------

  _openPanel() {
    this._fillPanel();
    this._panel.visible = true;
    this._open = true;
    this._row.visible = false; // the panel's own header takes over from the pill
    this._place();
    try {
      this._grab = Main.pushModal(this._panel, { actionMode: Shell.ActionMode.POPUP });
    } catch (e) {
      this._grab = null;
    }
    this._panel.grab_key_focus();
    // A click anywhere outside the pill and the panel closes it.
    this._outsideId = global.stage.connect('captured-event', (_s, ev) => {
      if (ev.type() !== Clutter.EventType.BUTTON_PRESS) return Clutter.EVENT_PROPAGATE;
      const target = global.stage.get_event_actor(ev);
      if (target && (this._panel.contains(target) || this._pill.contains(target)))
        return Clutter.EVENT_PROPAGATE;
      this._close();
      return Clutter.EVENT_STOP;
    });
  }

  _close() {
    if (!this._open) return;
    this._open = false;
    if (this._outsideId) {
      global.stage.disconnect(this._outsideId);
      this._outsideId = 0;
    }
    if (this._grab) {
      Main.popModal(this._grab);
      this._grab = null;
    }
    this._panel.visible = false;
    this._row.visible = true;
    this._place();
  }

  _fillPanel() {
    const m = this._model;
    const total = m.total;
    const st = m.status;
    const pick = (n) =>
      m.apps.slice(0, n).map((a) => ({
        appId: a.appId,
        name: a.name || a.appId,
        time: formatShort(a.ms),
        percent: total > 0 ? Math.round((a.ms * 100) / total) : 0,
      }));
    const days = this._week ?? [];
    const spent = days.filter((d) => d.ms > 0);
    const avg = spent.length ? spent.reduce((sum, d) => sum + d.ms, 0) / spent.length : 0;
    this._stats.update({
      dateLabel: GLib.DateTime.new_now_local().format('%a, %-d %b'),
      tracking: !this._client.connected
        ? 'Offline'
        : st?.paused
          ? 'Paused'
          : st?.idle
            ? 'Idle'
            : 'Tracking',
      totalMs: total,
      total: formatShort(total),
      appsUsed: m.apps.filter((a) => a.ms > 0).length,
      limit: this._totalLimit(),
      days,
      avg: formatShort(avg),
      apps: pick(5),
      paused: Boolean(st?.paused),
    });
    this._loadWeek();
  }

  _totalLimit() {
    for (const l of this._limits.values()) if (l.targetType === 'total') return l;
    return null;
  }

  async _loadWeek() {
    if (this._weekLoading) return;
    this._weekLoading = true;
    try {
      const now = new Date();
      const first = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 6);
      const last = new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1);
      const rows = await this._client.call('usage.summary', {
        from: first.getTime(),
        to: last.getTime(),
        groupBy: 'day',
      });
      const byKey = new Map((rows ?? []).map((r) => [r.key, r.ms]));
      const pad = (n) => String(n).padStart(2, '0');
      const week = Array.from({ length: 7 }, (_, i) => {
        const d = new Date(first.getFullYear(), first.getMonth(), first.getDate() + i);
        const key = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
        return {
          label: ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'][d.getDay()],
          ms: byKey.get(key) ?? 0,
          today: i === 6,
        };
      });
      const changed = JSON.stringify(week) !== JSON.stringify(this._week);
      this._week = week;
      if (changed && this._open) this._fillPanel();
    } catch (e) {
      console.error(`Screentime notch: ${e.message}`);
    }
    this._weekLoading = false;
  }

  async _saveLimit(minutes, overlay) {
    try {
      const l = this._totalLimit();
      await this._client.call('limits.set', {
        ...(l ? { id: l.id } : {}),
        targetType: 'total',
        target: 'all',
        dailyMs: minutes * 60000,
        action: overlay ? 'overlay' : 'notify',
      });
      await this._loadLimits();
    } catch (e) {
      console.error(`Screentime notch: ${e.message}`);
    }
    if (this._open) this._fillPanel();
  }

  async _removeLimit() {
    try {
      const l = this._totalLimit();
      if (l) await this._client.call('limits.delete', { id: l.id });
      await this._loadLimits();
    } catch (e) {
      console.error(`Screentime notch: ${e.message}`);
    }
    if (this._open) this._fillPanel();
  }

  async _togglePause() {
    try {
      if (this._model.status?.paused) await this._client.call('tracker.resume');
      else await this._client.call('tracker.pause', { minutes: 15 });
    } catch (e) {
      console.error(`Screentime notch: ${e.message}`);
    }
  }

  // ---- the daemon -------------------------------------------------------

  _onState(connected) {
    this._model.setConnected(connected);
    if (connected) this._resync();
    else if (this._minuteId) {
      GLib.source_remove(this._minuteId);
      this._minuteId = 0;
    }
    this._redraw();
  }

  async _resync() {
    const tz = -new Date().getTimezoneOffset();
    this._model.setOptions({ tzOffsetMin: tz });
    try {
      const [settings, status] = await Promise.all([
        this._client.call('settings.get'),
        this._client.call('tracker.status'),
      ]);
      this._applySettings(settings);
      this._model.setStatus(status, Math.floor(this._now()));
      await this._loadNames();
      await this._loadLimits();
      await this._refreshToday();
    } catch (e) {
      console.error(`Screentime notch: ${e.message}`);
    }
    this._redraw();
  }

  _applySettings(s) {
    if (s && typeof s.notchEnabled === 'boolean') this._enabled = s.notchEnabled;
  }

  async _loadNames() {
    for (const a of (await this._client.call('apps.list')) ?? [])
      if (a.name) this._names.set(a.appId, a.name);
  }

  async _loadLimits() {
    this._limits.clear();
    for (const l of (await this._client.call('limits.list')) ?? [])
      if (l.id != null) this._limits.set(l.id, l);
  }

  async _refreshToday() {
    const now = new Date();
    const midnight = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
    const rows = await this._client.call('usage.summary', {
      from: midnight,
      to: now.getTime() + 60000,
      groupBy: 'app',
    });
    let unknown = false;
    const apps = (rows ?? []).map((r) => {
      const name = this._names.get(r.key) ?? '';
      unknown = unknown || !name;
      return { appId: r.key, name, ms: r.ms };
    });
    this._model.setToday(apps);
    if (this._minuteId) GLib.source_remove(this._minuteId);
    this._minuteId = GLib.timeout_add_seconds(GLib.PRIORITY_DEFAULT, 60, () => {
      this._minuteId = 0;
      this._refreshToday().then(
        () => this._redraw(),
        () => {},
      );
      return GLib.SOURCE_REMOVE;
    });
    if (unknown) this._loadNames().catch(() => {});
  }

  _onNotification(method, params) {
    const now = Math.floor(this._now());
    switch (method) {
      case 'event.status':
        this._model.setStatus(params, now);
        this._refreshToday().then(
          () => this._redraw(),
          () => {},
        );
        break;
      case 'event.focus':
        this._refreshToday().then(
          () => this._redraw(),
          () => {},
        );
        return;
      case 'event.settings':
        this._applySettings(params);
        break;
      case 'event.reminder':
        this._model.reminder(params.kind, params.message, now);
        break;
      case 'event.limitHit':
        this._limitHit(params.limitId);
        return;
      default:
        return;
    }
    this._redraw();
  }

  async _limitHit(id) {
    try {
      if (!this._limits.has(id)) await this._loadLimits();
      const l = this._limits.get(id);
      if (!l) return;
      const target = l.targetType === 'app' ? (this._names.get(l.target) ?? l.target) : l.target;
      this._model.limitHit(target, l.dailyMs, Math.floor(this._now()));
    } catch (e) {
      console.error(`Screentime notch: ${e.message}`);
    }
    this._redraw();
  }
}
