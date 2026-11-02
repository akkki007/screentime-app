/**
 * The island's settings, opened from the Extensions app (or the gear in the
 * open island). A live preview at the top redraws as anything changes, with
 * the same colours and sizes the Shell uses (palette.js). Whether the island
 * is shown at all is the tracker's notchEnabled setting, changed here over the
 * tracker's socket like every other client.
 */
import Adw from 'gi://Adw';
import GLib from 'gi://GLib';
import Gio from 'gi://Gio';
import Gtk from 'gi://Gtk';
import Pango from 'gi://Pango';
import PangoCairo from 'gi://PangoCairo';
import { ExtensionPreferences } from 'resource:///org/gnome/Shell/Extensions/js/extensions/prefs.js';

import { DaemonClient } from './daemonClient.js';
import { ACCENTS, SHOULDER, SIZES, STYLES, STYLE_CHOICES, theme, unit } from './palette.js';

const APP_DESKTOP = 'io.github.akkki007.screentime.desktop';

/** A combo row bound to a string key. `choices` is [[value, label], ...]. */
function comboRow(settings, key, title, subtitle, choices) {
  const row = new Adw.ComboRow({
    title,
    subtitle: subtitle ?? '',
    model: Gtk.StringList.new(choices.map(([, label]) => label)),
  });
  const sync = () => {
    const i = choices.findIndex(([v]) => v === settings.get_string(key));
    if (i >= 0 && row.selected !== i) row.selected = i;
  };
  sync();
  row.connect('notify::selected', () => {
    const choice = choices[row.selected];
    if (choice && settings.get_string(key) !== choice[0]) settings.set_string(key, choice[0]);
  });
  settings.connect(`changed::${key}`, sync);
  return row;
}

function switchRow(settings, key, title, subtitle) {
  const row = new Adw.SwitchRow({ title, subtitle: subtitle ?? '' });
  settings.bind(key, row, 'active', Gio.SettingsBindFlags.DEFAULT);
  return row;
}

/** A switch that puts `value` in, or takes it out of, a string-list key. */
function listSwitchRow(settings, key, value, title, subtitle) {
  const row = new Adw.SwitchRow({ title, subtitle: subtitle ?? '' });
  const sync = () => {
    const on = settings.get_strv(key).includes(value);
    if (row.active !== on) row.active = on;
  };
  sync();
  row.connect('notify::active', () => {
    const list = settings.get_strv(key);
    const has = list.includes(value);
    if (row.active && !has) settings.set_strv(key, [...list, value]);
    if (!row.active && has)
      settings.set_strv(
        key,
        list.filter((v) => v !== value),
      );
  });
  settings.connect(`changed::${key}`, sync);
  return row;
}

/** Round colour swatches for the accent, one of them always picked. */
function accentRow(settings) {
  const row = new Adw.ActionRow({
    title: 'Accent colour',
    subtitle: 'For the limit ring, today and the focus timer',
  });
  const box = new Gtk.Box({ spacing: 6, valign: Gtk.Align.CENTER });
  let first = null;
  const buttons = new Map();
  for (const [key, a] of Object.entries(ACCENTS)) {
    const dot = new Gtk.DrawingArea({ content_width: 26, content_height: 26 });
    dot.set_draw_func((_a, cr, w, h) => {
      // mono follows the style's text colour; "match" is previewed on a dark bar
      const style = STYLES[settings.get_string('style')] ?? STYLES.black;
      const rgb = a.rgb ?? style.fg;
      const chosen = settings.get_string('accent') === key;
      const r = Math.min(w, h) / 2;
      cr.arc(w / 2, h / 2, chosen ? r - 5 : r - 1, 0, 2 * Math.PI);
      cr.setSourceRGBA(...unit(rgb));
      cr.fillPreserve();
      cr.setSourceRGBA(0, 0, 0, 0.15);
      cr.setLineWidth(1);
      cr.stroke();
      if (chosen) {
        // a ring around the chosen colour
        cr.arc(w / 2, h / 2, r - 1.5, 0, 2 * Math.PI);
        cr.setSourceRGBA(...unit(rgb));
        cr.setLineWidth(2);
        cr.stroke();
      }
      cr.$dispose();
    });
    const button = new Gtk.ToggleButton({
      child: dot,
      tooltip_text: a.label,
      css_classes: ['circular', 'flat'],
    });
    button.update_property([Gtk.AccessibleProperty.LABEL], [a.label]);
    if (first) button.set_group(first);
    else first = button;
    button.connect('toggled', () => {
      if (button.active && settings.get_string('accent') !== key)
        settings.set_string('accent', key);
    });
    buttons.set(key, { button, dot });
    box.append(button);
  }
  const sync = () => {
    const b = buttons.get(settings.get_string('accent')) ?? buttons.get('blue');
    if (!b.button.active) b.button.active = true;
    for (const { dot } of buttons.values()) dot.queue_draw();
  };
  sync();
  settings.connect('changed::accent', sync);
  // mono follows the style's text colour, so redraw the swatches with the style
  settings.connect('changed::style', () => {
    for (const { dot } of buttons.values()) dot.queue_draw();
  });
  row.add_suffix(box);
  return row;
}

/** A sketch of the top bar with the island on it, drawn the way the Shell will. */
function previewWidget(settings) {
  const area = new Gtk.DrawingArea({ content_height: 132, hexpand: true });
  area.set_draw_func((_a, cr, w, h) => {
    const cfg = {
      style: settings.get_string('style'),
      accent: settings.get_string('accent'),
      size: settings.get_string('size'),
    };
    // The preview can't see the Shell's bar, so "match" is shown on a typical dark one.
    const placement = settings.get_string('placement');
    const floating = placement === 'floating';
    const inBar = placement === 'top';
    // In the bar the island is a black notch, as in the Shell.
    const style = inBar && cfg.style === 'match' ? 'black' : cfg.style;
    const t = theme({ ...cfg, style, bar: [19, 19, 19, 255] });
    const z = t.size;
    const light = t.name === 'light';

    // a wallpaper and a top bar
    cr.rectangle(0, 0, w, h);
    cr.setSourceRGBA(...(light ? [0.86, 0.88, 0.92, 1] : [0.2, 0.22, 0.27, 1]));
    cr.fill();
    const bar = 28;
    cr.rectangle(0, 0, w, bar);
    // the same bar colour the "match" style is previewed against (a little lighter
    // under a notch in the bar, as a real bar is next to a black notch)
    cr.setSourceRGBA(...unit(light ? [246, 245, 244] : inBar ? [38, 38, 42] : [19, 19, 19]));
    cr.fill();
    const barText = (text, x) => {
      const l = PangoCairo.create_layout(cr);
      l.set_font_description(Pango.FontDescription.from_string('Sans 9'));
      l.set_text(text, -1);
      cr.setSourceRGBA(...unit(light ? [40, 40, 44] : [235, 235, 238]));
      cr.moveTo(x, (bar - 13) / 2);
      PangoCairo.show_layout(cr, l);
    };
    barText('Activities', 12);

    // what the closed island shows
    const left = settings.get_string('compact-left');
    const center = settings.get_string('compact-center') === 'clock' ? '14:05' : '2h 41m';
    const ring = settings.get_string('compact-right') === 'ring';
    const text = PangoCairo.create_layout(cr);
    text.set_font_description(Pango.FontDescription.from_string(`Sans Bold ${z.font * 0.75}`));
    text.set_text(center, -1);
    const [, logical] = text.get_pixel_extents();
    const gap = 8;
    const content =
      (left !== 'none' ? z.icon + gap : 0) + logical.width + (ring ? gap + z.icon : 0) + 32;
    const iw = Math.max(z.minWidth, content);
    const ih = inBar ? bar : z.height;
    const x = (w - iw) / 2;
    const y = inBar ? 0 : bar + (floating ? 6 : 0);
    const r = ih / 2 + (floating ? 0 : 3);

    cr.setSourceRGBA(...unit(t.bg));
    if (floating) {
      cr.newPath();
      cr.arc(x + r, y + r, r, Math.PI / 2, 1.5 * Math.PI);
      cr.arc(x + iw - r, y + r, r, 1.5 * Math.PI, Math.PI / 2);
      cr.closePath();
      cr.fill();
      if (light) {
        cr.setSourceRGBA(...unit(t.line));
        cr.setLineWidth(1);
        cr.newPath();
        cr.arc(x + r, y + r, r - 0.5, Math.PI / 2, 1.5 * Math.PI);
        cr.arc(x + iw - r, y + r, r - 0.5, 1.5 * Math.PI, Math.PI / 2);
        cr.closePath();
        cr.stroke();
      }
    } else {
      const s = SHOULDER;
      cr.newPath();
      cr.moveTo(x - s, y);
      cr.lineTo(x + iw + s, y);
      cr.arcNegative(x + iw + s, y + s, s, 1.5 * Math.PI, Math.PI);
      cr.lineTo(x + iw, y + ih - r);
      cr.arc(x + iw - r, y + ih - r, r, 0, Math.PI / 2);
      cr.lineTo(x + r, y + ih);
      cr.arc(x + r, y + ih - r, r, Math.PI / 2, Math.PI);
      cr.lineTo(x, y + s);
      cr.arcNegative(x - s, y + s, s, 0, -Math.PI / 2);
      cr.closePath();
      cr.fill();
    }

    let cx = x + 16;
    const cy = y + ih / 2;
    if (left !== 'none') {
      if (left === 'app') {
        // a stand-in app icon
        const s = z.icon;
        const rr = s * 0.28;
        cr.newPath();
        cr.arc(cx + s - rr, cy - s / 2 + rr, rr, -Math.PI / 2, 0);
        cr.arc(cx + s - rr, cy + s / 2 - rr, rr, 0, Math.PI / 2);
        cr.arc(cx + rr, cy + s / 2 - rr, rr, Math.PI / 2, Math.PI);
        cr.arc(cx + rr, cy - s / 2 + rr, rr, Math.PI, 1.5 * Math.PI);
        cr.closePath();
        cr.setSourceRGBA(0.36, 0.55, 0.98, 1);
        cr.fill();
      } else {
        cr.setSourceRGBA(...unit(t.fg));
        cr.setLineWidth(1.6);
        cr.newPath();
        cr.arc(cx + z.icon / 2, cy, z.icon / 2 - 2, 0, 2 * Math.PI);
        cr.stroke();
        cr.moveTo(cx + z.icon / 2, cy - z.icon / 4);
        cr.lineTo(cx + z.icon / 2, cy);
        cr.lineTo(cx + z.icon / 2 + z.icon / 6, cy + z.icon / 8);
        cr.stroke();
      }
      cx += z.icon + gap;
    }
    cr.setSourceRGBA(...unit(t.fg));
    cr.moveTo(cx, cy - logical.height / 2);
    PangoCairo.show_layout(cr, text);
    cx += logical.width + gap;
    if (ring) {
      const rc = z.icon / 2;
      const lw = Math.max(2.5, z.icon / 6);
      cr.setLineWidth(lw);
      cr.setLineCap(1);
      cr.newPath();
      cr.setSourceRGBA(...unit(t.fg, 0.16));
      cr.arc(cx + rc, cy, rc - lw / 2 - 1, 0, 2 * Math.PI);
      cr.stroke();
      cr.newPath();
      cr.setSourceRGBA(...unit(t.accent));
      cr.arc(cx + rc, cy, rc - lw / 2 - 1, -Math.PI / 2, -Math.PI / 2 + 2 * Math.PI * 0.62);
      cr.stroke();
    }
    cr.$dispose();
  });
  settings.connect('changed', () => area.queue_draw());
  const frame = new Gtk.Frame({ child: area, css_classes: ['card'], margin_bottom: 6 });
  frame.set_overflow(Gtk.Overflow.HIDDEN);
  return frame;
}

/** "Show the island": the tracker's notchEnabled, over its socket. */
function showRow(window) {
  const row = new Adw.SwitchRow({
    title: 'Show the island',
    subtitle: 'Connecting to Screentime…',
    sensitive: false,
  });
  let applying = false;
  const client = new DaemonClient({
    onNotification: (method, params) => {
      if (method === 'event.settings' && typeof params?.notchEnabled === 'boolean')
        set(params.notchEnabled);
    },
    onState: (connected) => {
      row.sensitive = connected;
      if (!connected) {
        row.subtitle = 'Screentime is not running. Start the app, then come back.';
        return;
      }
      row.subtitle = 'At the top centre of your screen';
      client.call('settings.get').then(
        (s) => set(Boolean(s?.notchEnabled)),
        () => {},
      );
    },
  });
  const set = (on) => {
    applying = true;
    row.active = on;
    applying = false;
  };
  row.connect('notify::active', () => {
    if (applying || !client.connected) return;
    client.call('settings.set', { notchEnabled: row.active }).catch(() => set(!row.active));
  });
  client.start();
  window.connect('close-request', () => {
    client.stop();
    return false;
  });
  return row;
}

export default class ScreentimePreferences extends ExtensionPreferences {
  fillPreferencesWindow(window) {
    const settings = this.getSettings();
    window.set_default_size(660, 860);
    window.search_enabled = false;

    const page = new Adw.PreferencesPage({
      title: 'Island',
      icon_name: 'preferences-desktop-appearance-symbolic',
    });
    window.add(page);

    const preview = new Adw.PreferencesGroup();
    preview.add(previewWidget(settings));
    page.add(preview);
    const show = new Adw.PreferencesGroup();
    show.add(showRow(window));
    page.add(show);

    const look = new Adw.PreferencesGroup({ title: 'Look' });
    look.add(
      comboRow(
        settings,
        'style',
        'Style',
        'Matching takes the top bar’s own colour, so they join without a seam',
        STYLE_CHOICES,
      ),
    );
    look.add(accentRow(settings));
    look.add(
      comboRow(
        settings,
        'size',
        'Size',
        null,
        Object.entries(SIZES).map(([k, z]) => [k, z.label]),
      ),
    );
    look.add(
      comboRow(
        settings,
        'placement',
        'Placement',
        'In the bar takes the clock’s place; Attached hangs from the bar; Floating floats below it',
        [
          ['top', 'In the bar'],
          ['attached', 'Attached'],
          ['floating', 'Floating'],
        ],
      ),
    );
    page.add(look);

    const shows = new Adw.PreferencesGroup({
      title: 'The closed island shows',
      description: 'Alerts, focus and pause take over the middle while they last.',
    });
    shows.add(
      comboRow(settings, 'compact-left', 'Left', null, [
        ['app', 'The app in front'],
        ['status', 'A status icon'],
        ['none', 'Nothing'],
      ]),
    );
    shows.add(
      comboRow(settings, 'compact-center', 'Middle', null, [
        ['total', "Today's screen time"],
        ['clock', 'The time'],
      ]),
    );
    shows.add(
      comboRow(settings, 'compact-right', 'Right', 'The ring appears once you set a daily limit', [
        ['ring', 'Daily limit ring'],
        ['none', 'Nothing'],
      ]),
    );
    page.add(shows);

    const sections = new Adw.PreferencesGroup({
      title: 'The open island shows',
      description: 'Pick the sections you want; the rest of the island makes room.',
    });
    sections.add(
      listSwitchRow(
        settings,
        'panel-sections',
        'media',
        'Now playing screen',
        'A music button in the open island opens it: Spotify, YouTube in your browser or any player, with its controls',
      ),
    );
    sections.add(
      listSwitchRow(
        settings,
        'panel-sections',
        'today',
        'Today',
        'Screen time, the daily limit and apps used',
      ),
    );
    sections.add(listSwitchRow(settings, 'panel-sections', 'week', 'Last 7 days'));
    sections.add(listSwitchRow(settings, 'panel-sections', 'apps', 'By app', 'Today’s top apps'));
    sections.add(
      listSwitchRow(
        settings,
        'panel-sections',
        'sites',
        'Top sites',
        'Today’s time per site, from the browser extension',
      ),
    );
    page.add(sections);

    const behaviour = new Adw.PreferencesGroup({ title: 'Behaviour' });
    behaviour.add(
      comboRow(settings, 'open-on', 'Open the island', 'Resting the pointer on it, or a click', [
        ['hover', 'On hover'],
        ['click', 'On click'],
      ]),
    );
    const delay = new Adw.SpinRow({
      title: 'Hover delay',
      subtitle: 'Milliseconds the pointer rests before it opens',
      adjustment: new Gtk.Adjustment({
        lower: 60,
        upper: 1000,
        step_increment: 10,
        page_increment: 50,
      }),
    });
    settings.bind('hover-delay', delay, 'value', Gio.SettingsBindFlags.DEFAULT);
    const hoverOnly = () => {
      delay.sensitive = settings.get_string('open-on') === 'hover';
    };
    hoverOnly();
    settings.connect('changed::open-on', hoverOnly);
    behaviour.add(delay);
    behaviour.add(
      comboRow(settings, 'motion', 'Motion', 'Your system’s reduce-motion setting always wins', [
        ['playful', 'Playful'],
        ['calm', 'Calm'],
        ['off', 'Off'],
      ]),
    );
    behaviour.add(
      comboRow(
        settings,
        'monitor',
        'Display',
        'With more than one, the main display or the one you are using',
        [
          ['primary', 'Main'],
          ['active', 'In use'],
        ],
      ),
    );
    behaviour.add(
      switchRow(
        settings,
        'reserve-space',
        'Keep windows clear of the island',
        'Maximised windows start below it instead of under it',
      ),
    );
    behaviour.add(
      switchRow(
        settings,
        'alerts',
        'Alerts in the island',
        'Limits reached and reminders open it for a few seconds',
      ),
    );
    page.add(behaviour);

    const personal = new Adw.PreferencesGroup({ title: 'Personal' });
    personal.add(
      switchRow(
        settings,
        'greeting',
        'Greet me',
        'Good morning, afternoon or evening, at your first activity of the day',
      ),
    );
    const name = new Adw.EntryRow({ title: 'What to call you', show_apply_button: true });
    const first = (GLib.get_real_name() === 'Unknown' ? '' : GLib.get_real_name()).split(/\s+/)[0];
    name.text = settings.get_string('nickname');
    if (first) name.set_tooltip_text(`Leave empty to use “${first}”`);
    name.connect('apply', () => settings.set_string('nickname', name.text.trim()));
    personal.add(name);
    const greetingOnly = () => {
      name.sensitive = settings.get_boolean('greeting');
    };
    greetingOnly();
    settings.connect('changed::greeting', greetingOnly);
    page.add(personal);

    const more = new Adw.PreferencesGroup();
    const open = new Adw.ActionRow({
      title: 'Open Screentime',
      subtitle: 'Limits, trends and the rest of your settings',
      activatable: true,
    });
    open.add_suffix(new Gtk.Image({ icon_name: 'go-next-symbolic' }));
    open.connect('activated', () => {
      try {
        Gio.DesktopAppInfo.new(APP_DESKTOP)?.launch([], null);
      } catch (e) {
        console.error(`Screentime: could not open the app: ${e.message}`);
      }
    });
    more.add(open);
    page.add(more);
  }
}
