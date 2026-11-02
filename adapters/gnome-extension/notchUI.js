/**
 * The island at the top of the screen (docs/adr/0014-top-of-screen-notch.md).
 *
 * One shape, three presentations: the closed island (today's total, the app
 * in front, a limit ring), a peek for alerts, and the open Screen Time panel.
 * The shape's width, height and corner radius follow damped springs
 * (spring.js), so it grows out of the top bar and settles with a little give;
 * the content is centred on the display, so it stays still while the shape
 * moves around it, and cross-fades between presentations.
 *
 * What the closed island says is decided by NotchModel (notch.js); how it
 * looks and behaves comes from the user's settings (schemas/, prefs.js). With
 * nothing happening it does no work: it redraws when the daemon says
 * something, when an alert ends, once a second during focus, once a minute for
 * today's total, and the springs run only while the shape is moving.
 */
import Clutter from 'gi://Clutter';
import GLib from 'gi://GLib';
import GObject from 'gi://GObject';
import Gio from 'gi://Gio';
import Shell from 'gi://Shell';
import St from 'gi://St';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';

import { DaemonClient } from './daemonClient.js';
import { Media } from './media.js';
import { NotchModel, formatShort } from './notch.js';
import { Ring, StatsPanel, appIcon } from './notchPanel.js';
import { PANEL_WIDTH, SHOULDER, css, theme, unit } from './palette.js';
import { MOTION, Spring } from './spring.js';

const COLLAPSE_MS = 300; // after the pointer leaves an island opened by hover
const HOVER_SLOP = 10; // px the pointer may drift while resting
const PEEK_MIN = 260;
const PEEK_MAX = 460;
const FLOAT_GAP = 6; // px between the top bar and a floating island
const RESTART_AFTER_S = 5;
const ORDER = { compact: 0, peek: 1, open: 2 };
const ALERT_KINDS = new Set(['alert', 'reminder', 'greeting']);
const DAEMON_UNIT = 'screentime-daemon.service';

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
  class ScreentimeIslandIcon extends St.DrawingArea {
    _init(size) {
      super._init({ width: size, height: size, y_align: Clutter.ActorAlign.CENTER });
      this._size = size;
      this._name = 'clock';
      this._rgb = [255, 255, 255];
    }

    setIcon(name, rgb) {
      this._name = ICONS[name] ? name : 'clock';
      if (rgb) this._rgb = rgb;
      this.queue_repaint();
    }

    vfunc_repaint() {
      const cr = this.get_context();
      const s = this._size / 24;
      cr.scale(s, s);
      cr.setSourceRGBA(...unit(this._rgb));
      cr.setLineWidth(2);
      cr.setLineCap(1); // round
      cr.setLineJoin(1);
      ICONS[this._name](cr);
      cr.$dispose();
    }
  },
);

/** The concave piece that joins the island to the top bar. `left` mirrors it. */
const Shoulder = GObject.registerClass(
  class ScreentimeIslandShoulder extends St.DrawingArea {
    _init(left) {
      super._init({ width: SHOULDER, height: SHOULDER });
      this._left = left;
      this._rgb = [0, 0, 0];
    }

    setColor(rgb) {
      this._rgb = rgb;
      this.queue_repaint();
    }

    vfunc_repaint() {
      const cr = this.get_context();
      cr.setSourceRGBA(...unit(this._rgb));
      if (this._left) {
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
  /** @param {{settings: Gio.Settings, openPrefs: () => void}} opts */
  constructor({ settings, openPrefs }) {
    this._settings = settings;
    this._openPrefs = openPrefs;
    this._realName = GLib.get_real_name() === 'Unknown' ? '' : GLib.get_real_name();
    this._model = new NotchModel({
      userName: this._realName,
      tzOffsetMin: -new Date().getTimezoneOffset(),
    });
    // Off until the tracker says otherwise: an island the user turned off (or
    // never turned on) must never flash at login or while the tracker is down.
    this._enabled = false;
    this._shown = false;
    this._limits = new Map();
    this._names = new Map();
    this._mode = 'compact';
    this._openedBy = null;
    this._timers = new Map();
    this._springs = { w: new Spring(0), h: new Spring(0), r: new Spring(0) };
    this._applied = { r: -1, floating: null };
    this._media = new Media(() => this._onMedia());
    this._page = 'stats'; // or 'media': which screen the open island shows
    this._mediaChoice = null; // the player picked by hand, if any
    this._closedAt = 0;
    this._sites = [];
    this._client = new DaemonClient({
      onNotification: (method, params) => this._onNotification(method, params),
      onState: (connected) => this._onState(connected),
    });
  }

  enable() {
    this._readSettings();
    this._build();
    this._settingsId = this._settings.connect('changed', (_s, key) => this._onSetting(key));
    this._monitorsId = Main.layoutManager.connect('monitors-changed', () => this._layout(true));
    // A new Shell theme (or dark/light switch) recolours the bar; follow it.
    this._barStyleId = Main.panel.connect('style-changed', () => {
      if (this._cfg.style !== 'match') return;
      this._readSettings();
      this._restyle();
      this._redraw();
    });
    this._focusId = global.display.connect('notify::focus-window', () => {
      if (this._cfg.monitor === 'active' && this._mode === 'compact') this._layout(true);
    });
    this._client.start();
    if (this._cfg.sections.includes('media')) this._media.start();
    this._redraw();
  }

  disable() {
    this._collapse(true);
    this._client.stop();
    this._media.stop();
    for (const id of this._timers.values()) GLib.source_remove(id);
    this._timers.clear();
    this._timeline?.stop();
    this._timeline = null;
    if (this._prewarmId) GLib.source_remove(this._prewarmId);
    this._prewarmId = 0;
    if (this._settingsId) this._settings.disconnect(this._settingsId);
    if (this._monitorsId) Main.layoutManager.disconnect(this._monitorsId);
    if (this._focusId) global.display.disconnect(this._focusId);
    if (this._barStyleId) Main.panel.disconnect(this._barStyleId);
    this._settingsId = this._monitorsId = this._focusId = this._barStyleId = 0;
    if (this._strut) {
      Main.layoutManager.removeChrome(this._strut);
      this._strut.destroy();
      this._strut = null;
    }
    if (this._clockHidden) {
      const clock = Main.panel.statusArea?.dateMenu?.container;
      if (clock) {
        clock.opacity = 255;
        clock.reactive = true;
      }
      this._clockHidden = false;
    }
    if (this._root) {
      Main.layoutManager.removeChrome(this._root);
      this._root.destroy();
      this._root = null;
    }
  }

  // ---- timers: one named slot each, so nothing is ever left behind ---------

  _later(name, ms, fn) {
    this._cancel(name);
    this._timers.set(
      name,
      GLib.timeout_add(GLib.PRIORITY_DEFAULT, Math.max(1, Math.round(ms)), () => {
        this._timers.delete(name);
        fn();
        return GLib.SOURCE_REMOVE;
      }),
    );
  }

  _cancel(name) {
    const id = this._timers.get(name);
    if (id) GLib.source_remove(id);
    this._timers.delete(name);
  }

  // ---- settings -----------------------------------------------------------

  _readSettings() {
    const s = this._settings;
    this._cfg = {
      style: s.get_string('style'),
      accent: s.get_string('accent'),
      size: s.get_string('size'),
      placement: s.get_string('placement'),
      left: s.get_string('compact-left'),
      center: s.get_string('compact-center'),
      right: s.get_string('compact-right'),
      openOn: s.get_string('open-on'),
      hoverDelay: s.get_int('hover-delay'),
      motion: s.get_string('motion'),
      monitor: s.get_string('monitor'),
      alerts: s.get_boolean('alerts'),
      reserve: s.get_boolean('reserve-space'),
      greeting: s.get_boolean('greeting'),
      nickname: s.get_string('nickname').trim(),
      sections: s.get_strv('panel-sections'),
    };
    // In the bar, an island the bar's own colour would be invisible: it is a black notch there.
    const style =
      this._cfg.placement === 'top' && this._cfg.style === 'match' ? 'black' : this._cfg.style;
    this._t = theme({ ...this._cfg, style, bar: this._barColor() });
    this._model.setOptions({ userName: this._cfg.nickname || this._realName });
    const params = MOTION[this._cfg.motion] ?? MOTION.playful;
    for (const sp of Object.values(this._springs)) sp.params = params;
  }

  _onSetting(key) {
    const before = this._cfg;
    this._readSettings();
    if (['style', 'accent', 'size'].includes(key)) this._restyle();
    if (key === 'open-on' && this._openedBy === 'hover' && this._cfg.openOn === 'click')
      this._collapse();
    if (key === 'panel-sections') {
      // Watch players only while their card is wanted.
      const wants = this._cfg.sections.includes('media');
      if (wants && !before.sections.includes('media')) this._media.start();
      if (!wants && before.sections.includes('media')) this._media.stop();
      if (this._cfg.sections.includes('sites')) this._refreshToday().catch(() => {});
      this._fillPanel(false);
      if (this._mode === 'open') this._layout();
    }
    if (before.placement !== this._cfg.placement) {
      this._applied.floating = null;
      this._restyle(); // the match style resolves differently in the bar
    }
    this._redraw();
  }

  /** The top bar's background as [r, g, b, a], for the match style; null before it is styled. */
  _barColor() {
    try {
      const c = Main.panel.get_theme_node().get_background_color();
      return [c.red, c.green, c.blue, c.alpha];
    } catch (_e) {
      return null;
    }
  }

  _animate() {
    return this._cfg.motion !== 'off' && St.Settings.get().enable_animations;
  }

  // ---- building -----------------------------------------------------------

  _build() {
    // root: sized to the shape every frame, so it never takes clicks meant for windows
    this._root = new St.Widget({ reactive: false, visible: false });
    this._leftShoulder = new Shoulder(true);
    this._rightShoulder = new Shoulder(false);
    this._body = new St.Widget({
      reactive: true,
      track_hover: true,
      can_focus: true,
      clip_to_allocation: true,
    });
    this._root.add_child(this._leftShoulder);
    this._root.add_child(this._rightShoulder);
    this._root.add_child(this._body);

    this._compact = this._buildCompact();
    this._peek = this._buildPeek();
    this._stats = new StatsPanel({
      onClose: () => this._collapse(),
      onSettings: () => {
        this._collapse(true);
        this._openPrefs();
      },
      onSaveLimit: (minutes, overlay) => this._saveLimit(minutes, overlay),
      onRemoveLimit: () => this._removeLimit(),
      onTogglePause: () => this._togglePause(),
      onMedia: (action, player) => this._media[action](player),
      onPage: (page) => this._turnTo(page),
      onPlayer: (player) => {
        this._mediaChoice = player;
        this._refreshPanel();
      },
    });
    this._panel = this._stats.actor;
    for (const layer of [this._compact, this._peek, this._panel]) {
      layer.opacity = 0;
      layer.visible = false;
      this._body.add_child(layer);
    }
    this._compact.visible = true;
    this._compact.opacity = 255;

    this._body.connect('notify::hover', () => this._onHover());
    this._body.connect('motion-event', () => {
      this._onMotion();
      return Clutter.EVENT_PROPAGATE;
    });
    this._body.connect('button-press-event', (_a, ev) => this._onPress(ev));
    this._body.connect('key-press-event', (_a, ev) => {
      if (ev.get_key_symbol() !== Clutter.KEY_Escape) return Clutter.EVENT_PROPAGATE;
      if (this._stats._popover) this._stats.closePopover();
      else this._collapse();
      return Clutter.EVENT_STOP;
    });

    this._restyle();
    Main.layoutManager.addTopChrome(this._root, { trackFullscreen: true });
  }

  /**
   * "Keep windows clear of the island": an invisible strip the height of the
   * closed island under the top bar, registered as a strut so maximised windows
   * start below it. Its own actor, so the island opening never moves windows.
   */
  _updateStrut() {
    const want = this._cfg.reserve && this._root?.visible && this._cfg.placement === 'attached';
    if (!want) {
      if (this._strut) {
        Main.layoutManager.removeChrome(this._strut);
        this._strut.destroy();
        this._strut = null;
      }
      return;
    }
    const { m, primary } = this._monitor();
    if (!m) return;
    if (!this._strut) {
      this._strut = new St.Widget({ reactive: false, opacity: 0 });
      Main.layoutManager.addTopChrome(this._strut, {
        affectsStruts: true,
        affectsInputRegion: false,
        trackFullscreen: true,
      });
    }
    // A strut only counts when it touches the screen edge, so it runs from the
    // top of the display (overlapping the bar) to the bottom of the closed island.
    const below = (primary ? Main.panel.height : 0) + this._t.size.height;
    this._strut.set_position(m.x, m.y);
    this._strut.set_size(m.width, below);
  }

  _buildCompact() {
    const z = this._t.size;
    const box = new St.BoxLayout({
      style: 'spacing: 8px; padding: 0 16px;',
      y_align: Clutter.ActorAlign.CENTER,
    });
    this._leftSlot = new St.Bin({ y_align: Clutter.ActorAlign.CENTER });
    this._statusIcon = new Icon(z.icon);
    this._text = new St.Label({ y_align: Clutter.ActorAlign.CENTER });
    this._detail = new St.Label({ y_align: Clutter.ActorAlign.CENTER });
    this._stopBtn = new St.Button({
      reactive: true,
      can_focus: true,
      track_hover: true,
      visible: false,
      accessible_name: 'Stop focus',
      y_align: Clutter.ActorAlign.CENTER,
      child: new St.Widget({
        width: 8,
        height: 8,
        x_align: Clutter.ActorAlign.CENTER,
        y_align: Clutter.ActorAlign.CENTER,
      }),
    });
    this._stopBtn.connect('clicked', () => {
      this._client
        .call('focus.stop')
        .catch((e) => console.error(`Screentime island: ${e.message}`));
      return Clutter.EVENT_STOP;
    });
    this._ring = new Ring(z.icon, Math.max(2.5, z.icon / 6));
    for (const a of [this._leftSlot, this._text, this._detail, this._stopBtn, this._ring])
      box.add_child(a);
    return box;
  }

  _buildPeek() {
    const box = new St.BoxLayout({ style: 'spacing: 12px; padding: 14px 22px 16px 22px;' });
    this._peekIcon = new Icon(24);
    const words = new St.BoxLayout({
      vertical: true,
      style: 'spacing: 2px;',
      y_align: Clutter.ActorAlign.CENTER,
    });
    this._peekTitle = new St.Label();
    this._peekDetail = new St.Label();
    this._peekDetail.clutter_text.line_wrap = true;
    this._peekDetail.clutter_text.ellipsize = 0; // Pango.EllipsizeMode.NONE: long messages wrap
    this._peekWords = words;
    words.add_child(this._peekTitle);
    words.add_child(this._peekDetail);
    box.add_child(this._peekIcon);
    box.add_child(words);
    return box;
  }

  /** Colours and sizes from the theme; called on build and whenever the look changes. */
  _restyle() {
    const t = this._t;
    const z = t.size;
    this._applied.r = -1;
    this._appliedKey = null;
    this._leftShoulder.setColor(t.bg);
    this._rightShoulder.setColor(t.bg);
    this._text.style = `color: ${css(t.fg)}; font-weight: 600; font-size: ${z.font}px; font-feature-settings: "tnum";`;
    this._detail.style = `color: ${css(t.muted)}; font-size: ${z.font - 1}px;`;
    this._stopBtn.style = `border-radius: 11px; width: 22px; height: 22px; background-color: ${css(t.raised)};`;
    this._stopBtn.child.style = `background-color: ${css(t.fg)}; border-radius: 2px;`;
    this._peekTitle.style = `color: ${css(t.fg)}; font-size: 15px; font-weight: 600;`;
    this._peekDetail.style = `color: ${css(t.muted)}; font-size: 13px;`;
    // the status icon and ring are sized at creation; a new size makes new ones
    if (this._statusIcon.width !== z.icon) {
      this._statusIcon.destroy();
      this._statusIcon = new Icon(z.icon);
      const old = this._ring;
      this._ring = new Ring(z.icon, Math.max(2.5, z.icon / 6));
      this._compact.replace_child(old, this._ring);
      old.destroy();
      this._appIconFor = null;
    }
    this._stats.setTheme(t);
  }

  // ---- geometry -------------------------------------------------------------

  _monitor() {
    const lm = Main.layoutManager;
    if (this._cfg.monitor === 'active' && this._mode === 'compact') {
      const w = global.display.get_focus_window();
      this._monitorIndex = w ? w.get_monitor() : lm.primaryIndex;
    }
    if (this._cfg.monitor !== 'active') this._monitorIndex = lm.primaryIndex;
    const i = this._monitorIndex ?? lm.primaryIndex;
    return { m: lm.monitors[i] ?? lm.primaryMonitor, primary: i === lm.primaryIndex };
  }

  /** Where the shape wants to be for the current presentation; also caches the content sizes. */
  _targets() {
    this._sizes = new Map();
    this._sizesId = (this._sizesId ?? 0) + 1;
    for (const layer of [this._compact, this._peek, this._panel]) {
      const [, lw] = layer.get_preferred_width(-1);
      const width = layer === this._panel ? PANEL_WIDTH : Math.ceil(lw);
      const [, lh] = layer.get_preferred_height(width);
      this._sizes.set(layer, [width, Math.ceil(lh)]);
    }
    const z = this._t.size;
    const floating = this._cfg.placement === 'floating';
    const ch = this._compactHeight();
    if (this._mode === 'open') {
      const [, h] = this._panel.get_preferred_height(PANEL_WIDTH);
      return { w: PANEL_WIDTH, h: Math.ceil(h), r: 30 };
    }
    if (this._mode === 'peek') {
      // Words on one line if they fit; otherwise wrap them at the widest the alert gets.
      this._peekWords.set_width(-1);
      const [, nw] = this._peek.get_preferred_width(-1);
      if (nw > PEEK_MAX) {
        const [, chrome] = this._peekIcon.get_preferred_width(-1);
        this._peekWords.set_width(PEEK_MAX - chrome - 12 - 44); // icon, spacing, side padding
      }
      const w = Math.max(PEEK_MIN, Math.min(PEEK_MAX, Math.ceil(nw)));
      this._peek.set_width(w);
      const [, h] = this._peek.get_preferred_height(w);
      return { w, h: Math.ceil(h), r: 24 };
    }
    const [, nw] = this._compact.get_preferred_width(-1);
    const w = Math.max(z.minWidth, Math.ceil(nw));
    return { w: w + (w % 2), h: ch, r: floating ? ch / 2 : ch / 2 + 3 };
  }

  /** Point the springs at the targets and run them (or jump, without motion). */
  _layout(jump = false) {
    if (!this._root?.visible) return;
    const t = this._targets();
    const s = this._springs;
    if (jump || !this._animate() || s.w.value === 0) {
      s.w.snap(t.w);
      s.h.snap(t.h);
      s.r.snap(t.r);
      this._apply();
      return;
    }
    const still = (sp, v) => sp.settled && sp.target === v;
    if (still(s.w, t.w) && still(s.h, t.h) && still(s.r, t.r)) {
      this._apply();
      return;
    }
    s.w.setTarget(t.w);
    s.h.setTarget(t.h);
    s.r.setTarget(t.r);
    this._run();
  }

  _run() {
    if (this._timeline) return;
    this._last = GLib.get_monotonic_time();
    this._timeline = new Clutter.Timeline({ actor: this._root, duration: 1000, repeat_count: -1 });
    this._timeline.connect('new-frame', () => {
      const now = GLib.get_monotonic_time();
      const dt = (now - this._last) / 1e6;
      this._last = now;
      const s = this._springs;
      const done = [s.w.step(dt), s.h.step(dt), s.r.step(dt)].every(Boolean);
      this._apply();
      if (done) {
        this._timeline.stop();
        this._timeline = null;
      }
    });
    this._timeline.start();
  }

  /** The closed island's height: the top bar's own height when it sits in the bar. */
  _compactHeight() {
    if (this._cfg.placement !== 'top') return this._t.size.height;
    const { primary } = this._monitor();
    return primary ? Main.panel.height : this._t.size.height;
  }

  /**
   * In the bar the island takes the clock's place, as a notch does, so the
   * clock is hidden while the island is shown there and comes back otherwise.
   */
  _updateClock() {
    const clock = Main.panel.statusArea?.dateMenu?.container;
    if (!clock) return;
    const hide = this._cfg.placement === 'top' && this._root?.visible && this._monitor().primary;
    if (hide && !this._clockHidden) {
      clock.opacity = 0;
      clock.reactive = false;
      this._clockHidden = true;
    } else if (!hide && this._clockHidden) {
      clock.opacity = 255;
      clock.reactive = true;
      this._clockHidden = false;
    }
  }

  /** Put the shape where the springs are this frame. */
  _apply() {
    const { m, primary } = this._monitor();
    if (!m) return;
    const floating = this._cfg.placement === 'floating';
    const w = Math.max(2, Math.round(this._springs.w.value / 2) * 2);
    const h = Math.max(2, Math.round(this._springs.h.value));
    const r = Math.max(
      0,
      Math.min(Math.round(this._springs.r.value), Math.floor(h / 2), Math.floor(w / 2)),
    );
    const inBar = this._cfg.placement === 'top';
    const top = inBar
      ? m.y
      : (primary ? m.y + Main.panel.height : m.y) + (floating ? FLOAT_GAP : 0);
    const sh = floating ? 0 : SHOULDER;
    // Frames that would draw the same whole-pixel shape change nothing: skip them.
    const key = `${w},${h},${r},${m.x},${m.width},${top},${floating},${this._mode},${this._sizesId}`;
    if (key === this._appliedKey) return;
    this._appliedKey = key;

    if (this._applied.floating !== floating) {
      this._leftShoulder.visible = !floating;
      this._rightShoulder.visible = !floating;
      this._applied.floating = floating;
      this._applied.r = -1;
    }
    if (this._applied.r !== r) {
      const t = this._t;
      const radius = floating ? `${r}px` : `0 0 ${r}px ${r}px`;
      const rim = t.name === 'light' && floating ? `border: 1px solid ${css(t.line)};` : '';
      this._body.style = `background-color: ${css(t.bg)}; border-radius: ${radius}; ${rim}`;
      this._applied.r = r;
    }
    this._root.set_size(w + 2 * sh, h);
    this._root.set_position(Math.round(m.x + (m.width - w) / 2 - sh), top);
    this._body.set_position(sh, 0);
    this._body.set_size(w, h);
    this._rightShoulder.set_position(sh + w, 0);
    this._leftShoulder.set_position(0, 0);

    // Content is centred on the display, not on the moving shape, so it stays still.
    const z = this._t.size;
    for (const layer of [this._compact, this._peek, this._panel]) {
      if (!layer.visible) continue;
      const [width, lh] = this._sizes?.get(layer) ?? [layer.width, layer.height];
      const lw = layer === this._peek ? layer.width : width;
      const y = layer === this._compact ? Math.round((this._compactHeight() - lh) / 2) : 0;
      layer.set_position(Math.round((w - lw) / 2), y);
    }
  }

  // ---- presentations ----------------------------------------------------------

  _setMode(mode) {
    if (mode === this._mode) return;
    const from = this._layerFor(this._mode);
    const to = this._layerFor(mode);
    const growing = ORDER[mode] > ORDER[this._mode];
    this._mode = mode;
    if (from !== to) this._crossFade(from, to, growing);
  }

  _layerFor(mode) {
    return mode === 'open' ? this._panel : mode === 'peek' ? this._peek : this._compact;
  }

  _crossFade(from, to, growing) {
    const slide = growing ? 8 : -8;
    from.remove_all_transitions();
    to.remove_all_transitions();
    if (!this._animate()) {
      from.opacity = 0;
      from.visible = false;
      to.visible = true;
      to.opacity = 255;
      to.translation_y = 0;
      return;
    }
    from.ease({
      opacity: 0,
      translation_y: -slide,
      duration: 140,
      mode: Clutter.AnimationMode.EASE_OUT_QUAD,
      onComplete: () => {
        from.visible = false;
        from.translation_y = 0;
      },
    });
    to.visible = true;
    to.opacity = 0;
    to.translation_y = slide;
    to.ease({
      opacity: 255,
      translation_y: 0,
      delay: 90,
      duration: 260,
      mode: Clutter.AnimationMode.EASE_OUT_QUAD,
    });
  }

  _openIsland(by) {
    if (this._mode === 'open') return;
    this._cancel('hover');
    this._cancel('collapse');
    this._openedBy = by;
    if (this._closedAt && Date.now() - this._closedAt > 5 * 60000) this._page = 'stats';
    this._fillPanel(); // cheap when the prewarmed panel is current
    this._setMode('open');
    this._layout();
    if (by === 'click') {
      try {
        this._grab = Main.pushModal(this._root, { actionMode: Shell.ActionMode.POPUP });
      } catch (_e) {
        this._grab = null;
      }
      this._body.grab_key_focus();
      // A click anywhere outside the island closes it. Under the grab every event
      // goes to the island's root (not through the stage), so listen there.
      this._outsideId = this._root.connect('captured-event', (_s, ev) => {
        if (ev.type() !== Clutter.EventType.BUTTON_PRESS) return Clutter.EVENT_PROPAGATE;
        // While the grab is held every event is delivered to the island, so test
        // where the click landed rather than which actor it was given to.
        const [x, y] = ev.get_coords();
        const [rx, ry] = this._root.get_transformed_position();
        const inside =
          x >= rx && x < rx + this._root.width && y >= ry && y < ry + this._root.height;
        if (inside) return Clutter.EVENT_PROPAGATE;
        this._collapse();
        return Clutter.EVENT_STOP;
      });
    }
  }

  _collapse(now = false) {
    this._cancel('collapse');
    if (this._outsideId) {
      this._root.disconnect(this._outsideId);
      this._outsideId = 0;
    }
    if (this._grab) {
      Main.popModal(this._grab);
      this._grab = null;
    }
    if (this._mode !== 'open') return;
    this._stats.closePopover();
    this._openedBy = null;
    this._closedAt = Date.now();
    this._setMode('compact');
    if (!now) this._redraw();
  }

  // ---- pointer --------------------------------------------------------------

  _onMotion() {
    // Resting on an alert holds it (see _redraw); only a click opens from there.
    if (this._mode !== 'compact' || this._cfg.openOn !== 'hover' || !this._enabled) return;
    if (this._timers.has('hover')) return;
    const [x, y] = global.get_pointer();
    this._restAt = [x, y];
    this._later('hover', this._cfg.hoverDelay, () => this._hoverRested());
  }

  /** Open only if the pointer stayed put: passing over the island on the way elsewhere leaves it closed. */
  _hoverRested() {
    if (!this._body.hover || this._mode === 'open') return;
    const [x, y] = global.get_pointer();
    const [x0, y0] = this._restAt;
    if (Math.hypot(x - x0, y - y0) <= HOVER_SLOP) this._openIsland('hover');
    else {
      this._restAt = [x, y];
      this._later('hover', this._cfg.hoverDelay, () => this._hoverRested());
    }
  }

  _onHover() {
    if (this._body.hover) {
      this._cancel('collapse');
      return;
    }
    this._cancel('hover');
    if (this._mode === 'open' && this._openedBy === 'hover')
      this._later('collapse', COLLAPSE_MS, () => this._collapse());
    if (this._mode === 'peek') this._redraw(); // an alert held by the pointer can now run out
  }

  _onPress(ev) {
    const button = ev.get_button();
    if (this._mode === 'open') return Clutter.EVENT_PROPAGATE; // the panel's own buttons handle it
    if (button === 3 && this._mode === 'peek') {
      this._model.temp = null; // right-click puts an alert away
      this._redraw();
      return Clutter.EVENT_STOP;
    }
    if (button === 2) {
      const m = this._media.current();
      if (m) this._media.playPause(m.name);
      return Clutter.EVENT_STOP;
    }
    if (button !== 1) return Clutter.EVENT_PROPAGATE;
    if (!this._client.connected) {
      this._startDaemon();
      return Clutter.EVENT_STOP;
    }
    if (this._mode === 'peek') this._model.temp = null;
    this._openIsland('click');
    return Clutter.EVENT_STOP;
  }

  // ---- drawing ----------------------------------------------------------------

  _now() {
    return GLib.get_real_time() / 1000;
  }

  _redraw() {
    if (!this._root) return;
    this._cancel('redraw');
    const now = Math.floor(this._now());
    // "Not running" only appears once the island has been shown this session
    // and the tracker then went away; an island that is off stays gone.
    this._shown = this._shown || (this._enabled && this._client.connected);
    const visible = this._shown && (this._enabled || !this._client.connected);
    const wasVisible = this._root.visible;
    this._root.visible = visible;
    if (!visible) {
      this._collapse(true);
      this._updateStrut();
      this._updateClock();
      return;
    }

    if (!this._cfg.greeting && this._model.temp?.view.kind === 'greeting') this._model.temp = null;
    // An alert under the pointer stays until the pointer leaves.
    if (this._mode === 'peek' && this._body.hover && this._model.temp)
      this._model.temp.until = Math.max(this._model.temp.until, now + 1500);

    const v = this._model.view(now);
    const alert = ALERT_KINDS.has(v.kind);
    this._drawCompact(v, alert && this._cfg.alerts);
    if (alert && this._cfg.alerts) this._drawPeek(v);
    if (this._mode === 'open') this._fillPanel();
    else {
      this._setMode(alert && this._cfg.alerts ? 'peek' : 'compact');
      this._prewarm();
    }
    this._layout(!wasVisible);
    this._updateStrut();
    this._updateClock();

    let next = this._model.nextChange(now);
    if ((this._cfg.center === 'clock' && v.kind === 'idle') || this._mode === 'open') {
      const minute = (Math.floor(now / 60000) + 1) * 60000;
      if (!next || minute < next) next = minute;
    }
    if (next > 0) this._later('redraw', Math.max(50, next - now), () => this._redraw());
  }

  _drawCompact(v, alertInPeek) {
    const t = this._t;
    const st = this._model.status;
    // While an alert peeks, the closed island underneath keeps its usual face.
    const face = alertInPeek ? this._idleFace() : v;
    const appId = st?.currentAppId;
    const showApp = this._cfg.left === 'app' && face.kind === 'idle' && appId && !st?.idle;

    if (this._cfg.left === 'none') this._leftSlot.visible = false;
    else {
      this._leftSlot.visible = true;
      if (showApp) {
        if (this._appIconFor !== appId) {
          this._appIconFor = appId;
          this._leftSlot.set_child(appIcon(appId, this._names.get(appId) ?? '', t.size.icon, t));
        }
      } else {
        this._appIconFor = null;
        if (this._leftSlot.child !== this._statusIcon) this._leftSlot.set_child(this._statusIcon);
        this._statusIcon.setIcon(face.icon, face.kind === 'focus' ? t.accent : t.fg);
      }
    }

    let text = face.text;
    if (face.kind === 'idle' && this._cfg.center === 'clock') text = this._clockText();
    this._text.text = text;
    this._detail.text = face.detail;
    this._detail.visible = Boolean(face.detail);
    this._stopBtn.visible = face.kind === 'focus';

    const limit = this._totalLimit();
    const ringOn = this._cfg.right === 'ring' && limit && face.kind === 'idle';
    this._ring.visible = Boolean(ringOn);
    if (ringOn) {
      const frac = this._model.total / limit.dailyMs;
      this._ring.set(frac, t.fg, frac >= 1 ? [255, 69, 58] : t.accent);
    }
  }

  _idleFace() {
    const now = Math.floor(this._now());
    const temp = this._model.temp;
    this._model.temp = null;
    const v = this._model.view(now);
    this._model.temp = temp;
    return v;
  }

  _drawPeek(v) {
    const t = this._t;
    const tint = v.kind === 'alert' ? [255, 69, 58] : v.kind === 'greeting' ? t.accent : t.fg;
    this._peekIcon.setIcon(v.icon, tint);
    this._peekTitle.text = v.text;
    this._peekDetail.text = v.detail;
    this._peekDetail.visible = Boolean(v.detail);
  }

  _clockText() {
    this._iface ??= new Gio.Settings({ schema_id: 'org.gnome.desktop.interface' });
    const twelve = this._iface.get_string('clock-format') === '12h';
    return GLib.DateTime.new_now_local().format(twelve ? '%-l:%M %p' : '%H:%M');
  }

  // ---- the panel ----------------------------------------------------------------

  /** Give the panel today's data; returns true if it had to be rebuilt. */
  _fillPanel(loadWeek = true) {
    const m = this._model;
    const total = m.total;
    const st = m.status;
    const days = this._week ?? [];
    const spent = days.filter((d) => d.ms > 0);
    const avg = spent.length ? spent.reduce((sum, d) => sum + d.ms, 0) / spent.length : 0;
    const rebuilt = this._stats.update({
      dateLabel: GLib.DateTime.new_now_local().format('%a, %-d %b'),
      timeLabel: this._clockText(),
      sections: this._cfg.sections,
      page: this._cfg.sections.includes('media') ? this._page : 'stats',
      media: this._cfg.sections.includes('media') ? this._mediaData() : null,
      players: this._cfg.sections.includes('media') ? this._players() : [],
      sites: this._sitesData(),
      totalMs: total,
      total: formatShort(total),
      appsUsed: m.apps.filter((a) => a.ms > 0).length,
      limit: this._totalLimit(),
      days,
      avg: formatShort(avg),
      apps: m.apps.slice(0, 4).map((a) => ({
        appId: a.appId,
        name: a.name || a.appId,
        time: formatShort(a.ms),
        percent: total > 0 ? Math.round((a.ms * 100) / total) : 0,
      })),
      paused: Boolean(st?.paused),
    });
    if (loadWeek) this._loadWeek();
    return rebuilt;
  }

  /** What is playing, trimmed to what the card draws (so unrelated changes don't rebuild it). */
  _mediaData() {
    const all = this._media.all();
    const m = all.find((p) => p.name === this._mediaChoice) ?? this._media.current();
    if (!m) return null;
    const { name, identity, desktopEntry, title, artist, artPath, status, canNext, canPrevious } =
      m;
    return { name, identity, desktopEntry, title, artist, artPath, status, canNext, canPrevious };
  }

  _sitesData() {
    if (!this._cfg.sections.includes('sites')) return [];
    const total = this._sites.reduce((n, r) => n + r.ms, 0);
    return this._sites.slice(0, 4).map((r) => ({
      domain: r.key,
      time: formatShort(r.ms),
      percent: total > 0 ? Math.round((r.ms * 100) / total) : 0,
    }));
  }

  /** The players to switch between, trimmed to what the switch draws. */
  _players() {
    // A fixed order, so the chips don't trade places under the pointer.
    return this._media
      .all()
      .map(({ name, identity, desktopEntry, status }) => ({ name, identity, desktopEntry, status }))
      .sort((a, b) => a.identity.localeCompare(b.identity) || a.name.localeCompare(b.name));
  }

  /** Show the other screen of the open island. */
  _turnTo(page) {
    this._page = page;
    this._refreshPanel();
  }

  /** Rebuild the panel if its data changed, and let the shape follow its new size. */
  _refreshPanel() {
    const rebuilt = this._fillPanel(false);
    if (rebuilt && this._mode === 'open') this._layout();
  }

  /** A player changed: refresh the card if it is open (or prebuilt), cheaply. */
  _onMedia() {
    if (!this._root) return;
    const rebuilt = this._fillPanel(false);
    if (rebuilt && this._mode === 'open') this._layout();
  }

  /** Build the panel ahead of time while closed, so opening never has to. */
  _prewarm() {
    if (!this._client.connected || this._prewarmId) return;
    const now = Date.now();
    if (this._prewarmedAt && now - this._prewarmedAt < 60000) return;
    this._prewarmId = GLib.idle_add(GLib.PRIORITY_LOW, () => {
      this._prewarmId = 0;
      this._prewarmedAt = Date.now();
      if (this._mode !== 'open' && this._root) this._fillPanel();
      return GLib.SOURCE_REMOVE;
    });
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
      if (changed && this._fillPanel(false) && this._mode === 'open') this._layout();
    } catch (e) {
      console.error(`Screentime island: ${e.message}`);
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
      console.error(`Screentime island: ${e.message}`);
    }
    this._redraw();
  }

  async _removeLimit() {
    try {
      const l = this._totalLimit();
      if (l) await this._client.call('limits.delete', { id: l.id });
      await this._loadLimits();
    } catch (e) {
      console.error(`Screentime island: ${e.message}`);
    }
    this._redraw();
  }

  async _togglePause() {
    try {
      if (this._model.status?.paused) await this._client.call('tracker.resume');
      else await this._client.call('tracker.pause', { minutes: 15 });
    } catch (e) {
      console.error(`Screentime island: ${e.message}`);
    }
  }

  // ---- the daemon ---------------------------------------------------------------

  /** Start the tracker's user service; harmless if it is running or not installed. */
  _startDaemon() {
    try {
      Gio.Subprocess.new(
        ['systemctl', '--user', 'start', DAEMON_UNIT],
        Gio.SubprocessFlags.STDOUT_SILENCE | Gio.SubprocessFlags.STDERR_SILENCE,
      );
    } catch (e) {
      console.error(`Screentime island: could not start the tracker: ${e.message}`);
    }
  }

  _onState(connected) {
    this._model.setConnected(connected);
    if (connected) {
      this._cancel('restart');
      this._restarted = false;
      this._resync();
    } else {
      this._cancel('minute');
      // Gone while the island was in use: try once to bring it back.
      if (this._shown && !this._restarted)
        this._later('restart', RESTART_AFTER_S * 1000, () => {
          this._restarted = true;
          if (!this._client.connected) this._startDaemon();
        });
    }
    this._redraw();
  }

  async _resync() {
    this._model.setOptions({ tzOffsetMin: -new Date().getTimezoneOffset() });
    try {
      const [settings, status] = await Promise.all([
        this._client.call('settings.get'),
        this._client.call('tracker.status'),
      ]);
      this._applyDaemonSettings(settings);
      this._model.setStatus(status, Math.floor(this._now()));
      await this._loadNames();
      await this._loadLimits();
      await this._refreshToday();
    } catch (e) {
      console.error(`Screentime island: ${e.message}`);
    }
    this._redraw();
  }

  _applyDaemonSettings(s) {
    if (s && typeof s.notchEnabled === 'boolean') {
      this._enabled = s.notchEnabled;
      // Turned off: forget that it was ever shown, so nothing of it comes back
      // (not even "not running") until it is turned on again.
      if (!this._enabled) this._shown = false;
    }
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
    if (this._cfg.sections.includes('sites')) {
      try {
        this._sites =
          (await this._client.call('usage.web', { from: midnight, to: now.getTime() + 60000 })) ??
          [];
      } catch (_e) {
        this._sites = [];
      }
    }
    this._later('minute', 60000, () => {
      this._refreshToday().then(
        () => this._redraw(),
        () => {},
      );
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
        this._applyDaemonSettings(params);
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
      console.error(`Screentime island: ${e.message}`);
    }
    this._redraw();
  }
}
