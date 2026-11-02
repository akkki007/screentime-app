/**
 * The Screen Time panel inside the open island (docs/adr/0014): the date,
 * today's total beside one overall daily limit, the apps used, the last seven
 * days and the apps by time, with a popover to set the daily limit. It only
 * draws what it is given, in the colours of the user's theme (palette.js);
 * notchUI.js loads the data, owns the island's shape and handles the buttons.
 */
import Clutter from 'gi://Clutter';
import GObject from 'gi://GObject';
import Shell from 'gi://Shell';
import St from 'gi://St';

import { formatShort } from './notch.js';
import { PANEL_WIDTH, css, theme as makeTheme, unit } from './palette.js';

const LIMIT_CHOICES = [60, 120, 240, 360, 480];
const SNAP = 15; // minutes per step on the − and + buttons

const label = (text, style = '', props = {}) => new St.Label({ text, style, ...props });
const row = (style = '', props = {}) => new St.BoxLayout({ style, ...props });
const col = (style = '', props = {}) => new St.BoxLayout({ vertical: true, style, ...props });

/** A ring: how much of the limit is used, in the accent colour. */
export const Ring = GObject.registerClass(
  class ScreentimeIslandRing extends St.DrawingArea {
    _init(size, width = 5) {
      super._init({ width: size, height: size, y_align: Clutter.ActorAlign.CENTER });
      this._size = size;
      this._width = width;
      this._fraction = 0;
      this._fg = [255, 255, 255];
      this._accent = [10, 132, 255];
    }

    set(fraction, fg, accent) {
      this._fraction = Math.max(0, Math.min(1, fraction));
      if (fg) this._fg = fg;
      if (accent) this._accent = accent;
      this.queue_repaint();
    }

    vfunc_repaint() {
      const cr = this.get_context();
      const c = this._size / 2;
      const r = c - this._width / 2 - 1;
      cr.setLineWidth(this._width);
      cr.setLineCap(1);
      cr.setSourceRGBA(...unit(this._fg, 0.16));
      cr.arc(c, c, r, 0, 2 * Math.PI);
      cr.stroke();
      if (this._fraction > 0) {
        cr.setSourceRGBA(...unit(this._accent));
        cr.arc(c, c, r, -Math.PI / 2, -Math.PI / 2 + 2 * Math.PI * this._fraction);
        cr.stroke();
      }
      cr.$dispose();
    }
  },
);

/** An app's icon from its .desktop file, or its initial on a tile. */
export function appIcon(appId, name, size, t) {
  const app =
    Shell.AppSystem.get_default().lookup_app(`${appId}.desktop`) ??
    Shell.AppSystem.get_default().lookup_app(appId);
  const gicon = app?.get_icon();
  if (gicon) return new St.Icon({ gicon, icon_size: size, y_align: Clutter.ActorAlign.CENTER });
  const letter = (name || appId || '?').trim().charAt(0).toUpperCase();
  return new St.Bin({
    width: size,
    height: size,
    y_align: Clutter.ActorAlign.CENTER,
    style: `border-radius: ${Math.round(size * 0.3)}px; background-color: ${css(t.raised)};`,
    child: label(
      letter,
      `color: ${css(t.fg)}; font-weight: 700; font-size: ${Math.round(size * 0.46)}px;`,
      {
        x_align: Clutter.ActorAlign.CENTER,
        y_align: Clutter.ActorAlign.CENTER,
      },
    ),
  });
}

/**
 * A round icon button for the header. `on`: the page it opens is showing;
 * `tint`: a colour for the icon (the accent while music plays).
 */
function iconButton(iconName, t, onClick, accessible, { on = false, tint = null } = {}) {
  const b = new St.Button({
    reactive: true,
    can_focus: true,
    track_hover: true,
    accessible_name: accessible,
    y_align: Clutter.ActorAlign.CENTER,
    child: new St.Icon({ icon_name: iconName, icon_size: 16 }),
  });
  const paint = () => {
    const fg = tint ?? (on || b.hover ? t.fg : t.muted);
    const bg = on ? t.raised : b.hover ? t.card : null;
    b.style = `border-radius: 16px; padding: 8px; color: ${css(fg)}; background-color: ${bg ? css(bg) : 'transparent'};`;
  };
  paint();
  b.connect('notify::hover', paint);
  b.connect('clicked', onClick);
  return b;
}

function switchActor(on, t, onToggle) {
  const track = new St.Button({
    reactive: true,
    can_focus: true,
    track_hover: true,
    width: 42,
    height: 24,
    y_align: Clutter.ActorAlign.CENTER,
  });
  const knob = new St.Widget({ width: 18, height: 18, y_align: Clutter.ActorAlign.CENTER });
  const bin = new St.Bin({ child: knob, x_expand: true, y_expand: true });
  track.set_child(bin);
  const paint = (state) => {
    track.style = `border-radius: 12px; background-color: ${css(state ? t.accent : t.track)};`;
    knob.style = `border-radius: 9px; background-color: ${state ? css(t.onAccent) : css(t.muted)}; margin: 0 3px;`;
    bin.x_align = state ? Clutter.ActorAlign.END : Clutter.ActorAlign.START;
  };
  let state = on;
  paint(state);
  track.connect('clicked', () => {
    state = !state;
    paint(state);
    onToggle(state);
  });
  return track;
}

export class StatsPanel {
  /**
   * @param {{onClose: () => void, onSettings: () => void, onSaveLimit: (minutes: number, overlay: boolean) => void,
   *          onRemoveLimit: () => void, onTogglePause: () => void,
   *          onMedia: (action: 'playPause' | 'next' | 'previous', player: string) => void,
   *          onPage: (page: 'stats' | 'media') => void, onPlayer: (player: string) => void}} h
   */
  constructor(h) {
    this._h = h;
    this._t = makeTheme();
    this.actor = new St.Widget({
      layout_manager: new Clutter.BinLayout(),
      reactive: true,
      can_focus: true,
      width: PANEL_WIDTH,
    });
    this._body = col('padding: 8px 18px 18px 18px;', {
      x_expand: true,
      y_align: Clutter.ActorAlign.START,
    });
    this._popover = null;
    this.actor.add_child(this._body);
    this._draft = { minutes: 60, overlay: true };
  }

  setTheme(t) {
    this._t = t;
    if (this._data) this.update(this._data, true);
  }

  /**
   * @param {{dateLabel: string, timeLabel: string, page: string, totalMs: number, total: string, appsUsed: number,
   *          limit: ?{id: number, dailyMs: number, action: string}, days: {label: string, ms: number, today: boolean}[],
   *          avg: string, apps: {appId: string, name: string, time: string, percent: number}[], paused: boolean}} d
   */
  update(d, force = false) {
    // Rebuilding is the costly part of opening, so skip it when nothing changed.
    const key = JSON.stringify(d);
    if (!force && key === this._key) return false;
    this._key = key;
    const turned = this._data && this._data.page !== d.page;
    this._data = d;
    this.closePopover();
    this._body.destroy_all_children();
    this._body.add_child(this._header(d));
    if (turned) this._slideIn(d.page === 'media' ? 1 : -1);
    if (d.page === 'media') {
      this._today = null;
      this._body.add_child(this._mediaScreen(d));
      return true;
    }
    // The sections the user chose (Settings → The open island shows), in a fixed order.
    const on = new Set(d.sections);
    const blocks = [];
    this._today = on.has('today') ? this._todayCard(d) : null;
    if (this._today) blocks.push(this._today);
    const small = [];
    if (on.has('week')) small.push(this._weekCard(d));
    if (on.has('apps')) small.push(this._appsCard(d));
    if (on.has('sites')) small.push(this._sitesCard(d));
    // the smaller cards two to a row; an odd one out takes the whole row
    for (let i = 0; i < small.length; i += 2) {
      const pair = row('spacing: 10px;', { x_expand: true });
      for (const c of small.slice(i, i + 2)) pair.add_child(c);
      blocks.push(pair);
    }
    blocks.forEach((b, i) => {
      if (i > 0) b.set_style(`${b.get_style() ?? ''} margin-top: 10px;`);
      this._body.add_child(b);
    });
    if (blocks.length === 0)
      this._body.add_child(
        label(
          'Nothing to show. Pick sections in the island’s settings.',
          `color: ${css(this._t.muted)}; font-size: 13px; padding: 6px 2px;`,
        ),
      );
    return true;
  }

  /** Turning to the other page: the new one slides in a little from its side. */
  _slideIn(dir) {
    const b = this._body;
    b.remove_all_transitions();
    if (!St.Settings.get().enable_animations) return;
    b.opacity = 0;
    b.translation_x = 14 * dir;
    b.ease({
      opacity: 255,
      translation_x: 0,
      duration: 220,
      mode: Clutter.AnimationMode.EASE_OUT_CUBIC,
    });
  }

  closePopover() {
    this._popover?.destroy();
    this._popover = null;
  }

  // ---- parts ------------------------------------------------------------

  _card(extra = '', props = {}) {
    return col(`background-color: ${css(this._t.card)}; border-radius: 20px; ${extra}`, props);
  }

  _header(d) {
    const t = this._t;
    const h = row('spacing: 8px; padding: 2px 0 12px 4px;', { y_align: Clutter.ActorAlign.CENTER });
    h.add_child(
      label(
        d.timeLabel,
        `color: ${css(t.fg)}; font-size: 18px; font-weight: 600; font-feature-settings: "tnum";`,
        {
          y_align: Clutter.ActorAlign.CENTER,
        },
      ),
    );
    h.add_child(
      label(d.dateLabel, `color: ${css(t.muted)}; font-size: 15px;`, {
        y_align: Clutter.ActorAlign.CENTER,
      }),
    );
    h.add_child(new St.Widget({ x_expand: true }));
    if (d.sections.includes('media')) {
      const music = d.page === 'media';
      h.add_child(
        iconButton(
          'audio-x-generic-symbolic',
          t,
          () => this._h.onPage(music ? 'stats' : 'media'),
          music ? 'Back to screen time' : 'Now playing',
          { on: music, tint: d.media?.status === 'Playing' ? t.accent : null },
        ),
      );
    }
    h.add_child(
      iconButton('emblem-system-symbolic', t, () => this._h.onSettings(), 'Island settings'),
    );
    return h;
  }

  _todayCard(d) {
    const t = this._t;
    const card = this._card('padding: 14px 18px 16px 18px; spacing: 8px;', { x_expand: true });
    card.add_child(label('Today', `color: ${css(t.muted)}; font-size: 13px;`));
    const cells = row('', { x_expand: true });

    const cell = (big, small) => {
      const c = col('spacing: 2px;', { x_expand: true, x_align: Clutter.ActorAlign.CENTER });
      c.add_child(
        label(
          big,
          `color: ${css(t.fg)}; font-size: 28px; font-weight: 600; font-feature-settings: "tnum";`,
          {
            x_align: Clutter.ActorAlign.CENTER,
          },
        ),
      );
      c.add_child(
        label(small, `color: ${css(t.muted)}; font-size: 12px;`, {
          x_align: Clutter.ActorAlign.CENTER,
        }),
      );
      return c;
    };
    const divider = () =>
      new St.Widget({ width: 1, style: `background-color: ${css(t.line)}; margin: 4px 0;` });
    cells.add_child(cell(d.total, 'Screen time'));
    cells.add_child(divider());

    // the limit: a ring, how far over or under, and a link that opens the popover
    const limitBox = row('spacing: 12px;', {
      x_expand: true,
      x_align: Clutter.ActorAlign.CENTER,
      y_align: Clutter.ActorAlign.CENTER,
    });
    const ring = new Ring(48);
    const limitMs = d.limit?.dailyMs ?? 0;
    ring.set(
      limitMs ? d.totalMs / limitMs : 0,
      t.fg,
      d.totalMs >= limitMs && limitMs ? [255, 69, 58] : t.accent,
    );
    limitBox.add_child(ring);
    const text = col('spacing: 2px;', { y_align: Clutter.ActorAlign.CENTER });
    if (limitMs) {
      const over = d.totalMs - limitMs;
      const head = row('spacing: 5px;', { y_align: Clutter.ActorAlign.END });
      head.add_child(
        label(
          over >= 0 ? `+${formatShort(over)}` : formatShort(-over),
          `color: ${css(t.fg)}; font-size: 22px; font-weight: 600;`,
        ),
      );
      head.add_child(
        label(over >= 0 ? 'over' : 'left', `color: ${css(t.muted)}; font-size: 13px;`, {
          y_align: Clutter.ActorAlign.END,
        }),
      );
      text.add_child(head);
    } else {
      text.add_child(label('No limit', `color: ${css(t.fg)}; font-size: 20px; font-weight: 600;`));
    }
    const edit = new St.Button({
      reactive: true,
      can_focus: true,
      track_hover: true,
      style: 'padding: 0;',
    });
    const editText = label(
      limitMs ? `of ${formatShort(limitMs)} limit ›` : 'Set a daily limit ›',
      `color: ${css(t.accent)}; font-size: 12px;`,
    );
    edit.set_child(editText);
    edit.connect('clicked', () => this._togglePopover());
    text.add_child(edit);
    limitBox.add_child(text);
    cells.add_child(limitBox);

    cells.add_child(divider());
    cells.add_child(cell(String(d.appsUsed), 'Apps used'));
    card.add_child(cells);
    return card;
  }

  _weekCard(d) {
    const t = this._t;
    const card = this._card('padding: 14px 16px 12px 16px; spacing: 6px;', { x_expand: true });
    const head = row('', { x_expand: true });
    head.add_child(
      label('Last 7 days', `color: ${css(t.muted)}; font-size: 13px;`, { x_expand: true }),
    );
    head.add_child(label(`avg ${d.avg}`, `color: ${css(t.muted)}; font-size: 12px;`));
    card.add_child(head);
    const max = Math.max(...d.days.map((x) => x.ms), 15 * 60000);
    const bars = row('spacing: 4px; margin-top: 4px;', { x_expand: true });
    for (const day of d.days) {
      const c = col('spacing: 5px;', { x_expand: true, x_align: Clutter.ActorAlign.FILL });
      const stage = new St.Widget({
        height: 104,
        x_expand: true,
        layout_manager: new Clutter.BinLayout(),
      });
      const h = day.ms > 0 ? Math.max(5, Math.round((day.ms / max) * 98)) : 3;
      stage.add_child(
        new St.Widget({
          width: day.ms > 0 ? 22 : 12,
          height: h,
          x_align: Clutter.ActorAlign.CENTER,
          y_align: Clutter.ActorAlign.END,
          style: `border-radius: ${day.ms > 0 ? '7px 7px 3px 3px' : '2px'}; background-color: ${day.today ? css(t.accent) : css(t.track)};`,
        }),
      );
      c.add_child(stage);
      c.add_child(
        label(
          day.label.charAt(0),
          `color: ${day.today ? css(t.fg) : css(t.muted)}; font-size: 11px;`,
          {
            x_align: Clutter.ActorAlign.CENTER,
          },
        ),
      );
      bars.add_child(c);
    }
    card.add_child(bars);
    return card;
  }

  _appsCard(d) {
    const t = this._t;
    const card = this._card('padding: 14px 10px 10px 10px; spacing: 2px;', { x_expand: true });
    card.add_child(
      label('By app', `color: ${css(t.muted)}; font-size: 13px; padding: 0 6px 4px 6px;`),
    );
    if (d.apps.length === 0)
      card.add_child(
        label(
          'Nothing tracked yet today.',
          `color: ${css(t.muted)}; font-size: 13px; padding: 8px 6px;`,
        ),
      );
    for (const a of d.apps.slice(0, 4)) {
      const item = row('spacing: 10px; padding: 5px 6px; border-radius: 12px;', {
        x_expand: true,
        reactive: true,
        track_hover: true,
      });
      item.connect('notify::hover', () => {
        item.style = `spacing: 10px; padding: 5px 6px; border-radius: 12px; background-color: ${item.hover ? css(t.raised) : 'transparent'};`;
      });
      item.add_child(appIcon(a.appId, a.name, 26, t));
      const mid = col('spacing: 5px;', { x_expand: true, y_align: Clutter.ActorAlign.CENTER });
      const name = label(a.name, `color: ${css(t.fg)}; font-size: 13px;`);
      name.clutter_text.ellipsize = 3;
      mid.add_child(name);
      const track = new St.Widget({
        height: 3,
        style: `background-color: ${css(t.track)}; border-radius: 2px;`,
      });
      const fill = new St.Widget({
        height: 3,
        style: `background-color: ${css(t.accent)}; border-radius: 2px;`,
      });
      track.add_child(fill);
      track.connect('notify::width', () =>
        fill.set_width(Math.max(3, (track.width * Math.max(a.percent, 1)) / 100)),
      );
      mid.add_child(track);
      item.add_child(mid);
      item.add_child(
        label(
          a.time,
          `color: ${css(t.fg)}; font-size: 13px; font-weight: 600; font-feature-settings: "tnum";`,
          {
            y_align: Clutter.ActorAlign.CENTER,
          },
        ),
      );
      card.add_child(item);
    }
    return card;
  }

  /** The art for a player: its cover when given as a local file, else the app's icon. */
  _art(m, size) {
    const t = this._t;
    const radius = Math.round(size * 0.13);
    if (m?.artPath)
      return new St.Widget({
        width: size,
        height: size,
        y_align: Clutter.ActorAlign.CENTER,
        style: `border-radius: ${radius}px; background-image: url("file://${m.artPath}"); background-size: cover;`,
      });
    const app = m?.desktopEntry
      ? Shell.AppSystem.get_default().lookup_app(`${m.desktopEntry}.desktop`)
      : null;
    const gicon = app?.get_icon();
    return new St.Bin({
      width: size,
      height: size,
      y_align: Clutter.ActorAlign.CENTER,
      style: `border-radius: ${radius}px; background-color: ${css(t.raised)};`,
      child: gicon
        ? new St.Icon({ gicon, icon_size: Math.round(size * 0.5) })
        : new St.Icon({
            icon_name: 'audio-x-generic-symbolic',
            icon_size: Math.round(size * 0.36),
            style: `color: ${css(t.muted)};`,
          }),
    });
  }

  /** The Now playing screen: the cover, the track, big controls, and a switch between players. */
  _mediaScreen(d) {
    const t = this._t;
    const m = d.media;
    const card = this._card('padding: 18px;', { x_expand: true });
    if (!m) {
      const empty = col('spacing: 8px; padding: 26px 0;', { x_expand: true });
      empty.add_child(
        new St.Icon({
          icon_name: 'audio-x-generic-symbolic',
          icon_size: 36,
          x_align: Clutter.ActorAlign.CENTER,
          style: `color: ${css(t.muted)};`,
        }),
      );
      empty.add_child(
        label('Nothing playing', `color: ${css(t.fg)}; font-size: 16px; font-weight: 600;`, {
          x_align: Clutter.ActorAlign.CENTER,
        }),
      );
      empty.add_child(
        label(
          'Play something in Spotify, or a video in your browser.',
          `color: ${css(t.muted)}; font-size: 13px;`,
          {
            x_align: Clutter.ActorAlign.CENTER,
          },
        ),
      );
      card.add_child(empty);
      return card;
    }

    const line = row('spacing: 20px;', { x_expand: true });
    line.add_child(this._art(m, 132));
    const words = col('spacing: 4px;', { x_expand: true, y_align: Clutter.ActorAlign.CENTER });
    const app = row('spacing: 6px;');
    const appGicon = m.desktopEntry
      ? Shell.AppSystem.get_default().lookup_app(`${m.desktopEntry}.desktop`)?.get_icon()
      : null;
    if (appGicon)
      app.add_child(
        new St.Icon({ gicon: appGicon, icon_size: 14, y_align: Clutter.ActorAlign.CENTER }),
      );
    app.add_child(
      label(m.identity, `color: ${css(t.muted)}; font-size: 12px;`, {
        y_align: Clutter.ActorAlign.CENTER,
      }),
    );
    words.add_child(app);
    const title = label(
      m.title || m.identity,
      `color: ${css(t.fg)}; font-size: 21px; font-weight: 700;`,
    );
    title.clutter_text.ellipsize = 3;
    words.add_child(title);
    if (m.artist) {
      const by = label(m.artist, `color: ${css(t.muted)}; font-size: 15px;`);
      by.clutter_text.ellipsize = 3;
      words.add_child(by);
    }

    const control = (icon, accessible, action, enabled, size) => {
      const primary = action === 'playPause';
      const b = new St.Button({
        reactive: enabled,
        can_focus: enabled,
        track_hover: true,
        accessible_name: accessible,
        width: size,
        height: size,
        y_align: Clutter.ActorAlign.CENTER,
        child: new St.Icon({ icon_name: icon, icon_size: primary ? 22 : 18 }),
      });
      const paint = () => {
        const bg = primary ? t.accent : b.hover && enabled ? t.raised : null;
        const fg = primary ? t.onAccent : enabled ? t.fg : t.track;
        b.style = `border-radius: ${size / 2}px; color: ${css(fg)}; background-color: ${bg ? css(bg) : 'transparent'};`;
      };
      paint();
      b.connect('notify::hover', paint);
      b.connect('clicked', () => this._h.onMedia(action, m.name));
      return b;
    };
    const playing = m.status === 'Playing';
    const controls = row('spacing: 10px; margin-top: 14px;', {
      y_align: Clutter.ActorAlign.CENTER,
    });
    controls.add_child(
      control('media-skip-backward-symbolic', 'Previous', 'previous', m.canPrevious, 44),
    );
    controls.add_child(
      control(
        playing ? 'media-playback-pause-symbolic' : 'media-playback-start-symbolic',
        playing ? 'Pause' : 'Play',
        'playPause',
        true,
        56,
      ),
    );
    controls.add_child(control('media-skip-forward-symbolic', 'Next', 'next', m.canNext, 44));
    words.add_child(controls);
    line.add_child(words);
    card.add_child(line);

    // More than one player (say Spotify and YouTube in a browser): pick which one to control.
    if (d.players.length > 1) {
      const pick = row('spacing: 6px; margin-top: 14px;', { x_expand: true });
      for (const p of d.players) {
        const chosen = p.name === m.name;
        const chip = row('spacing: 6px;');
        const g = p.desktopEntry
          ? Shell.AppSystem.get_default().lookup_app(`${p.desktopEntry}.desktop`)?.get_icon()
          : null;
        if (g)
          chip.add_child(
            new St.Icon({ gicon: g, icon_size: 14, y_align: Clutter.ActorAlign.CENTER }),
          );
        chip.add_child(
          label(p.identity, `color: ${css(chosen ? t.fg : t.muted)}; font-size: 12px;`, {
            y_align: Clutter.ActorAlign.CENTER,
          }),
        );
        if (p.status === 'Playing')
          chip.add_child(
            new St.Widget({
              width: 6,
              height: 6,
              y_align: Clutter.ActorAlign.CENTER,
              style: `border-radius: 3px; background-color: ${css(t.accent)};`,
            }),
          );
        const b = new St.Button({
          reactive: true,
          can_focus: true,
          track_hover: true,
          child: chip,
          accessible_name: `Control ${p.identity}`,
        });
        const paint = () => {
          const bg = chosen ? t.raised : b.hover ? t.card : null;
          b.style = `border-radius: 14px; padding: 6px 12px; background-color: ${bg ? css(bg) : 'transparent'}; border: 1px solid ${css(t.line)};`;
        };
        paint();
        b.connect('notify::hover', paint);
        b.connect('clicked', () => this._h.onPlayer(p.name));
        pick.add_child(b);
      }
      card.add_child(pick);
    }
    return card;
  }

  _sitesCard(d) {
    const t = this._t;
    const card = this._card('padding: 14px 10px 10px 10px; spacing: 2px;', { x_expand: true });
    card.add_child(
      label('Top sites', `color: ${css(t.muted)}; font-size: 13px; padding: 0 6px 4px 6px;`),
    );
    if (d.sites.length === 0)
      card.add_child(
        label(
          'No sites yet. The browser extension adds time per site.',
          `color: ${css(t.muted)}; font-size: 13px; padding: 8px 6px;`,
        ),
      );
    for (const site of d.sites.slice(0, 4)) {
      const item = row('spacing: 10px; padding: 5px 6px; border-radius: 12px;', { x_expand: true });
      item.add_child(
        new St.Bin({
          width: 26,
          height: 26,
          y_align: Clutter.ActorAlign.CENTER,
          style: `border-radius: 8px; background-color: ${css(t.raised)};`,
          child: new St.Icon({
            icon_name: 'web-browser-symbolic',
            icon_size: 15,
            style: `color: ${css(t.muted)};`,
          }),
        }),
      );
      const mid = col('spacing: 5px;', { x_expand: true, y_align: Clutter.ActorAlign.CENTER });
      const name = label(site.domain, `color: ${css(t.fg)}; font-size: 13px;`);
      name.clutter_text.ellipsize = 3;
      mid.add_child(name);
      const track = new St.Widget({
        height: 3,
        style: `background-color: ${css(t.track)}; border-radius: 2px;`,
      });
      const fill = new St.Widget({
        height: 3,
        style: `background-color: ${css(t.accent)}; border-radius: 2px;`,
      });
      track.add_child(fill);
      track.connect('notify::width', () =>
        fill.set_width(Math.max(3, (track.width * Math.max(site.percent, 1)) / 100)),
      );
      mid.add_child(track);
      item.add_child(mid);
      item.add_child(
        label(
          site.time,
          `color: ${css(t.fg)}; font-size: 13px; font-weight: 600; font-feature-settings: "tnum";`,
          {
            y_align: Clutter.ActorAlign.CENTER,
          },
        ),
      );
      card.add_child(item);
    }
    return card;
  }

  // ---- the daily limit popover -------------------------------------------

  _togglePopover() {
    if (this._popover) {
      this.closePopover();
      return;
    }
    const d = this._data;
    this._draft = {
      minutes: d.limit ? Math.round(d.limit.dailyMs / 60000) : 60,
      overlay: d.limit ? d.limit.action !== 'notify' : true,
    };
    this._popover = this._buildPopover();
    this.actor.add_child(this._popover);
  }

  _buildPopover() {
    const t = this._t;
    const d = this._data;
    const pop = col(
      `spacing: 12px; padding: 16px 18px 14px 18px; background-color: ${css(t.raised)}; border-radius: 20px; border: 1px solid ${css(t.line)};`,
      {
        width: 320,
        x_align: Clutter.ActorAlign.CENTER,
        y_align: Clutter.ActorAlign.START,
        reactive: true,
      },
    );
    // just under the Today card, wherever the chosen sections put it
    const below = this._today ? Math.round(this._today.y + this._today.height) - 6 : 112;
    pop.set_style(`${pop.style} margin-top: ${below}px;`);
    pop.add_child(label('Daily limit', `color: ${css(t.muted)}; font-size: 13px;`));

    const value = label(
      '',
      `color: ${css(t.fg)}; font-size: 30px; font-weight: 600; font-feature-settings: "tnum";`,
      {
        x_align: Clutter.ActorAlign.CENTER,
        x_expand: true,
      },
    );
    const chips = [];
    const fmt = (m) =>
      m % 60 === 0
        ? `${m / 60}h`
        : `${Math.floor(m / 60) ? `${Math.floor(m / 60)}h ` : ''}${m % 60}m`;
    const paint = () => {
      value.text = fmt(this._draft.minutes);
      for (const [mins, chip] of chips) {
        const on = mins === this._draft.minutes;
        chip.style = `border-radius: 12px; padding: 5px 10px; font-size: 13px; color: ${on ? css(t.onAccent) : css(t.muted)}; background-color: ${on ? css(t.accent) : 'transparent'};`;
      }
    };
    const step = (delta) => () => {
      this._draft.minutes = Math.max(SNAP, Math.min(720, this._draft.minutes + delta));
      paint();
    };
    const round = (text, onClick) => {
      const b = new St.Button({
        label: text,
        reactive: true,
        can_focus: true,
        track_hover: true,
        width: 36,
        height: 36,
        style: `border-radius: 18px; background-color: ${css(t.card)}; color: ${css(t.fg)}; font-size: 20px;`,
      });
      b.connect('clicked', onClick);
      return b;
    };
    const stepper = row('', { x_expand: true, y_align: Clutter.ActorAlign.CENTER });
    stepper.add_child(round('−', step(-SNAP)));
    stepper.add_child(value);
    stepper.add_child(round('+', step(SNAP)));
    pop.add_child(stepper);

    const chipRow = row(
      `spacing: 2px; padding-bottom: 10px; border-bottom: 1px solid ${css(t.line)};`,
      { x_expand: true },
    );
    for (const mins of LIMIT_CHOICES) {
      const chip = new St.Button({
        label: fmt(mins),
        reactive: true,
        can_focus: true,
        track_hover: true,
        x_expand: true,
      });
      chip.connect('clicked', () => {
        this._draft.minutes = mins;
        paint();
      });
      chips.push([mins, chip]);
      chipRow.add_child(chip);
    }
    pop.add_child(chipRow);
    paint();

    const option = (text, on, onToggle) => {
      const r = row('', { x_expand: true, y_align: Clutter.ActorAlign.CENTER });
      r.add_child(
        label(text, `color: ${css(t.fg)}; font-size: 13px;`, {
          x_expand: true,
          y_align: Clutter.ActorAlign.CENTER,
        }),
      );
      r.add_child(switchActor(on, t, onToggle));
      return r;
    };
    pop.add_child(
      option('Show a reminder at the limit', this._draft.overlay, (v) => {
        this._draft.overlay = v;
      }),
    );
    pop.add_child(option('Pause tracking', d.paused, () => this._h.onTogglePause()));

    const actions = row('padding-top: 2px;', {
      x_expand: true,
      y_align: Clutter.ActorAlign.CENTER,
    });
    const remove = new St.Button({
      label: d.limit ? 'Remove limit' : 'Cancel',
      reactive: true,
      can_focus: true,
      track_hover: true,
      x_expand: true,
      x_align: Clutter.ActorAlign.START,
      style: `color: ${css(t.muted)}; font-size: 13px; padding: 0;`,
    });
    remove.connect('clicked', () => {
      this.closePopover();
      if (d.limit) this._h.onRemoveLimit();
    });
    const save = new St.Button({
      label: 'Save',
      reactive: true,
      can_focus: true,
      track_hover: true,
      style: `background-color: ${css(t.accent)}; color: ${css(t.onAccent)}; border-radius: 16px; padding: 7px 22px; font-size: 13px; font-weight: 600;`,
    });
    save.connect('clicked', () => {
      this.closePopover();
      this._h.onSaveLimit(this._draft.minutes, this._draft.overlay);
    });
    actions.add_child(remove);
    actions.add_child(save);
    pop.add_child(actions);
    return pop;
  }
}
