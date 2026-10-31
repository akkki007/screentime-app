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

/** The logo: a rounded square with a clock, in the theme's text colour. */
const Logo = GObject.registerClass(
  class ScreentimeIslandLogo extends St.DrawingArea {
    _init(t) {
      super._init({ width: 30, height: 30, y_align: Clutter.ActorAlign.CENTER });
      this._t = t;
    }

    vfunc_repaint() {
      const cr = this.get_context();
      const t = this._t;
      cr.setSourceRGBA(...unit(t.fg));
      const r = 9;
      const s = 30;
      cr.arc(s - r, r, r, -Math.PI / 2, 0);
      cr.arc(s - r, s - r, r, 0, Math.PI / 2);
      cr.arc(r, s - r, r, Math.PI / 2, Math.PI);
      cr.arc(r, r, r, Math.PI, 1.5 * Math.PI);
      cr.closePath();
      cr.fill();
      cr.setSourceRGBA(...unit(t.bg));
      cr.setLineWidth(2.2);
      cr.setLineCap(1);
      cr.arc(15, 15, 7, 0, 2 * Math.PI);
      cr.stroke();
      cr.moveTo(15, 11);
      cr.lineTo(15, 15);
      cr.lineTo(18, 16.8);
      cr.stroke();
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

function iconButton(iconName, t, onClick, accessible) {
  const b = new St.Button({
    reactive: true,
    can_focus: true,
    track_hover: true,
    accessible_name: accessible,
    style: `border-radius: 15px; padding: 7px; color: ${css(t.muted)};`,
    child: new St.Icon({ icon_name: iconName, icon_size: 16 }),
  });
  b.connect('notify::hover', () => {
    b.style = `border-radius: 15px; padding: 7px; color: ${css(b.hover ? t.fg : t.muted)}; background-color: ${b.hover ? css(t.raised) : 'transparent'};`;
  });
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
   *          onRemoveLimit: () => void, onTogglePause: () => void}} h
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
   * @param {{dateLabel: string, tracking: string, totalMs: number, total: string, appsUsed: number,
   *          limit: ?{id: number, dailyMs: number, action: string}, days: {label: string, ms: number, today: boolean}[],
   *          avg: string, apps: {appId: string, name: string, time: string, percent: number}[], paused: boolean}} d
   */
  update(d, force = false) {
    // Rebuilding is the costly part of opening, so skip it when nothing changed.
    const key = JSON.stringify(d);
    if (!force && key === this._key) return false;
    this._key = key;
    this._data = d;
    this.closePopover();
    this._body.destroy_all_children();
    this._body.add_child(this._header(d));
    this._body.add_child(this._todayCard(d));
    const grid = row('spacing: 10px; margin-top: 10px;', { x_expand: true });
    grid.add_child(this._weekCard(d));
    grid.add_child(this._appsCard(d));
    this._body.add_child(grid);
    return true;
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
    const h = row('spacing: 10px; padding: 0 0 10px 2px;', { y_align: Clutter.ActorAlign.CENTER });
    h.add_child(new Logo(t));
    h.add_child(
      label('Screen Time', `color: ${css(t.fg)}; font-size: 17px; font-weight: 600;`, {
        y_align: Clutter.ActorAlign.CENTER,
      }),
    );
    h.add_child(
      label(d.dateLabel, `color: ${css(t.muted)}; font-size: 15px;`, {
        y_align: Clutter.ActorAlign.CENTER,
      }),
    );
    h.add_child(new St.Widget({ x_expand: true }));
    const dot =
      d.tracking === 'Tracking'
        ? [48, 209, 88]
        : d.tracking === 'Paused'
          ? [255, 159, 10]
          : t.muted;
    const chip = row(
      `spacing: 6px; padding: 4px 10px; border-radius: 12px; background-color: ${css(t.raised)};`,
      { y_align: Clutter.ActorAlign.CENTER },
    );
    chip.add_child(
      new St.Widget({
        width: 7,
        height: 7,
        y_align: Clutter.ActorAlign.CENTER,
        style: `border-radius: 4px; background-color: ${css(dot)};`,
      }),
    );
    chip.add_child(label(d.tracking, `color: ${css(t.fg)}; font-size: 12px;`));
    h.add_child(chip);
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
    pop.set_style(`${pop.style} margin-top: 112px;`);
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
