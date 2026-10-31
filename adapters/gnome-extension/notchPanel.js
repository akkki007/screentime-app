/**
 * The Screen Time panel that drops from the notch (docs/adr/0014): the date,
 * today's total beside one overall daily limit, the apps used, the last seven
 * days and the apps by time, with a popover to set the daily limit. It only
 * draws what it is given; notchUI.js loads the data and handles the buttons.
 */
import Clutter from 'gi://Clutter';
import GObject from 'gi://GObject';
import Shell from 'gi://Shell';
import St from 'gi://St';

import { formatShort } from './notch.js';

const WIDTH = 780;
const MUTED = 'rgb(150,143,137)';
const CARD = 'background-color: rgb(24,22,21); border-radius: 22px;';
const LIMIT_CHOICES = [60, 120, 240, 360, 480];
const SNAP = 15; // minutes per step on the − and + buttons

const label = (text, style = '', props = {}) => new St.Label({ text, style, ...props });
const row = (style = '', props = {}) => new St.BoxLayout({ style, ...props });
const col = (style = '', props = {}) => new St.BoxLayout({ vertical: true, style, ...props });

/** The ring beside the limit: how much of it is used, filled in white. */
const Ring = GObject.registerClass(
  class ScreentimeNotchRing extends St.DrawingArea {
    _init(size) {
      super._init({ width: size, height: size, y_align: Clutter.ActorAlign.CENTER });
      this._size = size;
      this._fraction = 0;
    }

    set(fraction) {
      this._fraction = Math.max(0, Math.min(1, fraction));
      this.queue_repaint();
    }

    vfunc_repaint() {
      const cr = this.get_context();
      const c = this._size / 2;
      const r = c - 4;
      cr.setLineWidth(5);
      cr.setLineCap(1);
      cr.setSourceRGBA(1, 1, 1, 0.14);
      cr.arc(c, c, r, 0, 2 * Math.PI);
      cr.stroke();
      if (this._fraction > 0) {
        cr.setSourceRGBA(1, 1, 1, 1);
        cr.arc(c, c, r, -Math.PI / 2, -Math.PI / 2 + 2 * Math.PI * this._fraction);
        cr.stroke();
      }
      cr.$dispose();
    }
  },
);

/** The logo: a white rounded square with a clock, like the app's. */
const Logo = GObject.registerClass(
  class ScreentimeNotchLogo extends St.DrawingArea {
    _init() {
      super._init({ width: 34, height: 34, y_align: Clutter.ActorAlign.CENTER });
    }

    vfunc_repaint() {
      const cr = this.get_context();
      cr.setSourceRGBA(0.96, 0.93, 0.9, 1);
      const r = 10;
      cr.arc(34 - r, r, r, -Math.PI / 2, 0);
      cr.arc(34 - r, 34 - r, r, 0, Math.PI / 2);
      cr.arc(r, 34 - r, r, Math.PI / 2, Math.PI);
      cr.arc(r, r, r, Math.PI, 1.5 * Math.PI);
      cr.closePath();
      cr.fill();
      cr.setSourceRGBA(0.08, 0.07, 0.06, 1);
      cr.setLineWidth(2.4);
      cr.setLineCap(1);
      cr.arc(17, 17, 8, 0, 2 * Math.PI);
      cr.stroke();
      cr.moveTo(17, 12.5);
      cr.lineTo(17, 17);
      cr.lineTo(20.5, 19);
      cr.stroke();
      cr.$dispose();
    }
  },
);

function switchActor(on, onToggle) {
  const track = new St.Button({
    reactive: true,
    can_focus: true,
    track_hover: true,
    width: 46,
    height: 26,
    y_align: Clutter.ActorAlign.CENTER,
  });
  const knob = new St.Widget({ width: 20, height: 20, y_align: Clutter.ActorAlign.CENTER });
  const bin = new St.Bin({ child: knob, x_expand: true, y_expand: true });
  track.set_child(bin);
  const paint = (state) => {
    track.style = `border-radius: 13px; background-color: ${state ? 'rgb(245,238,230)' : 'rgb(70,64,59)'};`;
    knob.style = `border-radius: 10px; background-color: ${state ? 'rgb(12,10,9)' : 'rgb(150,143,137)'}; margin: 0 3px;`;
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

function appIcon(appId, name) {
  const app = Shell.AppSystem.get_default().lookup_app(`${appId}.desktop`);
  const gicon = app?.get_icon();
  if (gicon) return new St.Icon({ gicon, icon_size: 30, y_align: Clutter.ActorAlign.CENTER });
  const letter = (name || appId || '?').trim().charAt(0).toUpperCase();
  const box = new St.Bin({
    width: 30,
    height: 30,
    y_align: Clutter.ActorAlign.CENTER,
    style: 'border-radius: 9px; background-color: rgb(72,64,58);',
    child: label(letter, 'color: white; font-weight: 700; font-size: 14px;', {
      x_align: Clutter.ActorAlign.CENTER,
      y_align: Clutter.ActorAlign.CENTER,
    }),
  });
  return box;
}

export class StatsPanel {
  /**
   * @param {{onClose: () => void, onSaveLimit: (minutes: number, overlay: boolean) => void,
   *          onRemoveLimit: () => void, onTogglePause: () => void}} h
   */
  constructor(h) {
    this._h = h;
    this.actor = new St.Widget({
      layout_manager: new Clutter.BinLayout(),
      reactive: true,
      visible: false,
      can_focus: true,
      width: WIDTH,
    });
    // One black window holds everything, joined to the top bar above it.
    this._body = col(
      'background-color: rgb(0,0,0); border-radius: 0 0 30px 30px; padding: 6px 16px 16px 16px;',
      { x_expand: true, y_align: Clutter.ActorAlign.START },
    );
    this._popover = null;
    this.actor.add_child(this._body);
    this._draft = { minutes: 60, overlay: true };
  }

  /**
   * @param {{dateLabel: string, tracking: string, totalMs: number, total: string, appsUsed: number,
   *          limit: ?{id: number, dailyMs: number, action: string}, days: {label: string, ms: number, today: boolean}[],
   *          avg: string, apps: {appId: string, name: string, time: string, percent: number}[], paused: boolean}} d
   */
  update(d) {
    this._data = d;
    this._body.destroy_all_children();
    this._body.add_child(this._header(d));
    const grid = row('spacing: 12px; margin-top: 10px;');
    const left = col('spacing: 12px;', { width: 462 });
    left.add_child(this._todayCard(d));
    left.add_child(this._weekCard(d));
    grid.add_child(left);
    grid.add_child(this._appsCard(d));
    this._body.add_child(grid);
  }

  closePopover() {
    this._popover?.destroy();
    this._popover = null;
  }

  // ---- parts ------------------------------------------------------------

  _header(d) {
    const h = row('spacing: 12px; padding: 2px 4px 0 4px;', { y_align: Clutter.ActorAlign.CENTER });
    h.add_child(new Logo());
    h.add_child(
      label('Screen Time', 'color: white; font-size: 20px; font-weight: 600;', {
        y_align: Clutter.ActorAlign.CENTER,
      }),
    );
    h.add_child(
      new St.Widget({
        width: 1,
        height: 22,
        style: 'background-color: rgb(58,52,47);',
        y_align: Clutter.ActorAlign.CENTER,
      }),
    );
    h.add_child(
      label(d.dateLabel, `color: ${MUTED}; font-size: 18px;`, {
        y_align: Clutter.ActorAlign.CENTER,
      }),
    );
    const spacer = new St.Widget({ x_expand: true });
    h.add_child(spacer);
    h.add_child(
      label(
        d.tracking,
        'color: white; font-size: 13px; background-color: rgb(36,33,31); border-radius: 14px; padding: 5px 12px;',
        {
          y_align: Clutter.ActorAlign.CENTER,
        },
      ),
    );
    const close = new St.Button({
      reactive: true,
      can_focus: true,
      track_hover: true,
      style: `color: ${MUTED}; font-size: 20px; padding: 0 6px;`,
      label: '✕',
    });
    close.connect('clicked', () => this._h.onClose());
    h.add_child(close);
    return h;
  }

  _todayCard(d) {
    const card = col(`${CARD} padding: 14px 18px 16px 18px; spacing: 8px;`);
    card.add_child(label('Today', `color: ${MUTED}; font-size: 13px;`));
    const cells = row('', { x_expand: true });

    const cell = (big, small, extra = '') => {
      const c = col(`spacing: 2px; ${extra}`, {
        x_expand: true,
        x_align: Clutter.ActorAlign.CENTER,
      });
      c.add_child(
        label(big, 'color: white; font-size: 30px; font-weight: 600;', {
          x_align: Clutter.ActorAlign.CENTER,
        }),
      );
      c.add_child(
        label(small, `color: ${MUTED}; font-size: 13px;`, { x_align: Clutter.ActorAlign.CENTER }),
      );
      return c;
    };
    cells.add_child(cell(d.total, 'Screen time'));
    cells.add_child(
      new St.Widget({ width: 1, style: 'background-color: rgb(46,42,39); margin: 4px 0;' }),
    );

    // the limit: a ring, how far over or under, and a pencil that opens the popover
    const limitBox = row('spacing: 12px;', {
      x_expand: true,
      x_align: Clutter.ActorAlign.CENTER,
      y_align: Clutter.ActorAlign.CENTER,
    });
    const ring = new Ring(52);
    const limitMs = d.limit?.dailyMs ?? 0;
    ring.set(limitMs ? d.totalMs / limitMs : 0);
    limitBox.add_child(ring);
    const text = col('spacing: 2px;', { y_align: Clutter.ActorAlign.CENTER });
    if (limitMs) {
      const over = d.totalMs - limitMs;
      const head = row('spacing: 6px;', { y_align: Clutter.ActorAlign.END });
      head.add_child(
        label(
          over >= 0 ? `+${formatShort(over)}` : formatShort(-over),
          'color: white; font-size: 24px; font-weight: 600;',
        ),
      );
      head.add_child(
        label(over >= 0 ? 'over' : 'left', `color: ${MUTED}; font-size: 14px;`, {
          y_align: Clutter.ActorAlign.END,
        }),
      );
      text.add_child(head);
    } else {
      text.add_child(label('No limit', 'color: white; font-size: 22px; font-weight: 600;'));
    }
    const edit = new St.Button({
      reactive: true,
      can_focus: true,
      track_hover: true,
      style: `color: ${MUTED}; font-size: 13px; padding: 0;`,
    });
    edit.set_child(
      label(
        limitMs ? `of ${formatShort(limitMs)} limit  ✎` : 'Set a daily limit  ✎',
        `color: ${MUTED}; font-size: 13px;`,
      ),
    );
    edit.connect('clicked', () => this._togglePopover());
    text.add_child(edit);
    limitBox.add_child(text);
    cells.add_child(limitBox);

    cells.add_child(
      new St.Widget({ width: 1, style: 'background-color: rgb(46,42,39); margin: 4px 0;' }),
    );
    cells.add_child(cell(String(d.appsUsed), 'Apps used'));
    card.add_child(cells);
    return card;
  }

  _weekCard(d) {
    const card = col(`${CARD} padding: 14px 18px 14px 18px; spacing: 6px;`);
    const head = row('', { x_expand: true });
    head.add_child(label('Last 7 days', `color: ${MUTED}; font-size: 13px;`, { x_expand: true }));
    head.add_child(label(`avg ${d.avg}`, `color: ${MUTED}; font-size: 13px;`));
    card.add_child(head);
    const max = Math.max(...d.days.map((x) => x.ms), 15 * 60000);
    const bars = row('spacing: 6px;', { x_expand: true });
    for (const day of d.days) {
      const c = col('spacing: 4px;', { x_expand: true, x_align: Clutter.ActorAlign.FILL });
      const stage = new St.Widget({
        height: 92,
        x_expand: true,
        layout_manager: new Clutter.BinLayout(),
      });
      if (day.ms > 0) {
        const h = Math.max(4, Math.round((day.ms / max) * 84));
        stage.add_child(
          new St.Widget({
            width: 30,
            height: h,
            x_align: Clutter.ActorAlign.CENTER,
            y_align: Clutter.ActorAlign.END,
            style: `border-radius: 9px 9px 4px 4px; background-color: ${day.today ? 'rgb(245,238,230)' : 'rgb(74,67,61)'};`,
          }),
        );
      } else {
        stage.add_child(
          new St.Widget({
            width: 14,
            height: 2,
            x_align: Clutter.ActorAlign.CENTER,
            y_align: Clutter.ActorAlign.END,
            style: 'background-color: rgb(58,52,47);',
          }),
        );
      }
      c.add_child(stage);
      c.add_child(
        label(day.label, `color: ${day.today ? 'white' : MUTED}; font-size: 12px;`, {
          x_align: Clutter.ActorAlign.CENTER,
        }),
      );
      bars.add_child(c);
    }
    card.add_child(bars);
    return card;
  }

  _appsCard(d) {
    const card = col(`${CARD} padding: 14px 14px 12px 14px; spacing: 8px;`, { x_expand: true });
    card.add_child(label('By app', `color: ${MUTED}; font-size: 13px; padding-left: 4px;`));
    if (d.apps.length === 0)
      card.add_child(
        label('Nothing tracked yet today.', `color: ${MUTED}; font-size: 13px; padding: 8px 4px;`),
      );
    d.apps.forEach((a, i) => {
      const item = row(
        `spacing: 10px; padding: 6px 8px; border-radius: 14px; ${i === 0 ? 'background-color: rgb(36,33,31);' : ''}`,
        { x_expand: true },
      );
      item.add_child(appIcon(a.appId, a.name));
      const mid = col('spacing: 5px;', { x_expand: true, y_align: Clutter.ActorAlign.CENTER });
      const name = label(a.name, 'color: white; font-size: 14px;');
      name.clutter_text.ellipsize = 3;
      mid.add_child(name);
      const track = new St.Widget({
        height: 3,
        style: 'background-color: rgb(58,52,47); border-radius: 2px;',
      });
      const fill = new St.Widget({
        height: 3,
        style: 'background-color: rgb(245,238,230); border-radius: 2px;',
      });
      track.add_child(fill);
      track.connect('notify::width', () =>
        fill.set_width(Math.max(3, (track.width * Math.max(a.percent, 1)) / 100)),
      );
      mid.add_child(track);
      item.add_child(mid);
      const nums = col('spacing: 0;', { y_align: Clutter.ActorAlign.CENTER });
      nums.add_child(
        label(a.time, 'color: white; font-size: 14px; font-weight: 600;', {
          x_align: Clutter.ActorAlign.END,
        }),
      );
      nums.add_child(
        label(a.percent === 0 ? '<1%' : `${a.percent}%`, `color: ${MUTED}; font-size: 11px;`, {
          x_align: Clutter.ActorAlign.END,
        }),
      );
      item.add_child(nums);
      card.add_child(item);
    });
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
    const d = this._data;
    const pop = col(
      'spacing: 12px; padding: 16px 18px 14px 18px; background-color: rgb(20,18,17); border-radius: 22px; border: 1px solid rgb(48,43,39);',
      {
        width: 330,
        x_align: Clutter.ActorAlign.START,
        y_align: Clutter.ActorAlign.START,
        reactive: true,
      },
    );
    pop.set_style(`${pop.style} margin-left: 110px; margin-top: 118px;`);
    pop.add_child(label('Daily limit', `color: ${MUTED}; font-size: 14px;`));

    const value = label('', 'color: white; font-size: 34px; font-weight: 600;', {
      x_align: Clutter.ActorAlign.CENTER,
      x_expand: true,
    });
    const chips = [];
    const fmt = (m) =>
      m % 60 === 0
        ? `${m / 60}h`
        : `${Math.floor(m / 60) ? `${Math.floor(m / 60)}h ` : ''}${m % 60}m`;
    const paint = () => {
      value.text = fmt(this._draft.minutes);
      for (const [mins, chip] of chips) {
        const on = mins === this._draft.minutes;
        chip.style = `border-radius: 15px; padding: 6px 12px; font-size: 14px; color: ${on ? 'white' : MUTED}; border: 2px solid ${on ? 'rgb(235,228,220)' : 'transparent'};`;
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
        width: 40,
        height: 40,
        style:
          'border-radius: 20px; background-color: rgb(36,33,31); color: rgb(190,183,176); font-size: 22px;',
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
      'spacing: 4px; padding-bottom: 10px; border-bottom: 1px solid rgb(44,40,37);',
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
        label(text, 'color: white; font-size: 14px;', {
          x_expand: true,
          y_align: Clutter.ActorAlign.CENTER,
        }),
      );
      r.add_child(switchActor(on, onToggle));
      return r;
    };
    pop.add_child(
      option('Show a reminder at the limit', this._draft.overlay, (v) => {
        this._draft.overlay = v;
      }),
    );
    pop.add_child(option('Notification at the limit', true, () => {}));
    pop.add_child(option('Pause tracking', d.paused, () => this._h.onTogglePause()));

    const actions = row('padding-top: 4px;', {
      x_expand: true,
      y_align: Clutter.ActorAlign.CENTER,
    });
    const remove = new St.Button({
      label: 'Remove limit',
      reactive: true,
      can_focus: true,
      track_hover: true,
      x_expand: true,
      x_align: Clutter.ActorAlign.START,
      style: `color: ${MUTED}; font-size: 14px; padding: 0;`,
    });
    remove.connect('clicked', () => {
      this.closePopover();
      this._h.onRemoveLimit();
    });
    const save = new St.Button({
      label: 'Save',
      reactive: true,
      can_focus: true,
      track_hover: true,
      style:
        'background-color: rgb(245,238,230); color: rgb(12,10,9); border-radius: 18px; padding: 8px 24px; font-size: 14px; font-weight: 600;',
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
