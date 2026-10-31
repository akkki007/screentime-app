/**
 * The island's looks, shared by the Shell (notchUI.js, notchPanel.js) and the
 * settings window (prefs.js) so the preview is exactly what the island draws.
 * Pure JavaScript, no GNOME imports, so Bun can test it. Colours are [r, g, b]
 * in 0-255.
 */

/** Island styles. `black` joins a black top bar seamlessly; `light` a light one. */
export const STYLES = {
  black: {
    label: 'Black',
    bg: [0, 0, 0],
    fg: [255, 255, 255],
    muted: [150, 150, 156],
    card: [22, 22, 24],
    raised: [34, 34, 37],
    track: [54, 54, 58],
    line: [44, 44, 48],
  },
  graphite: {
    label: 'Graphite',
    bg: [30, 30, 32],
    fg: [255, 255, 255],
    muted: [154, 154, 160],
    card: [44, 44, 47],
    raised: [56, 56, 60],
    track: [70, 70, 75],
    line: [58, 58, 62],
  },
  light: {
    label: 'Light',
    bg: [255, 255, 255],
    fg: [28, 29, 33],
    muted: [102, 105, 114],
    card: [245, 245, 247],
    raised: [236, 236, 240],
    track: [226, 226, 231],
    line: [233, 233, 237],
  },
};

/** Accent colours for rings, today's bar and the focus timer. */
export const ACCENTS = {
  blue: { label: 'Blue', rgb: [10, 132, 255] },
  green: { label: 'Green', rgb: [48, 209, 88] },
  orange: { label: 'Orange', rgb: [255, 159, 10] },
  pink: { label: 'Pink', rgb: [255, 55, 95] },
  purple: { label: 'Purple', rgb: [191, 90, 242] },
  teal: { label: 'Teal', rgb: [100, 210, 255] },
  mono: { label: 'Mono', rgb: null }, // the style's own text colour
};

/** Closed-island sizes: pill height, text and icon sizes. */
export const SIZES = {
  small: { label: 'Small', height: 30, font: 12, icon: 16, minWidth: 116 },
  regular: { label: 'Regular', height: 34, font: 13, icon: 18, minWidth: 132 },
  large: { label: 'Large', height: 40, font: 15, icon: 21, minWidth: 156 },
};

/** The shoulders that flare into the top bar, in px. */
export const SHOULDER = 12;
/** The open island's width. */
export const PANEL_WIDTH = 640;

/** Labels for every style choice, including the one that follows the top bar. */
export const STYLE_CHOICES = [
  ['match', 'Match top bar'],
  ...Object.entries(STYLES).map(([k, s]) => [k, s.label]),
];

const mix = (a, b, t) => a.map((v, i) => v + (b[i] - v) * t);

/**
 * A style made from the top bar's own colour, so the island joins it without
 * a seam. A see-through bar (blur extensions) gives no colour to match, so it
 * falls back to black. Light bars get dark text, dark bars light text.
 */
export function styleFromBar(rgba) {
  if (!rgba || (rgba[3] ?? 255) < 250) return { ...STYLES.black, label: 'Match the top bar' };
  const bg = rgba.slice(0, 3);
  const light = luminance(bg) > 0.45;
  const base = light ? STYLES.light : STYLES.black;
  const toward = light ? [0, 0, 0] : [255, 255, 255];
  return {
    ...base,
    label: 'Match the top bar',
    bg,
    card: mix(bg, toward, light ? 0.04 : 0.07),
    raised: mix(bg, toward, light ? 0.075 : 0.12),
    track: mix(bg, toward, light ? 0.12 : 0.2),
    line: mix(bg, toward, light ? 0.09 : 0.15),
  };
}

/**
 * Everything the island needs to draw, from the user's settings. Unknown
 * values fall back. `bar` is the top bar's [r, g, b, a], for the match style.
 */
export function theme({ style = 'match', accent = 'blue', size = 'regular', bar = null } = {}) {
  const s = style === 'match' ? styleFromBar(bar) : (STYLES[style] ?? STYLES.black);
  const a = ACCENTS[accent] ?? ACCENTS.blue;
  const z = SIZES[size] ?? SIZES.regular;
  return {
    ...s,
    name:
      style === 'match'
        ? luminance(s.bg) > 0.45
          ? 'light'
          : 'black'
        : STYLES[style]
          ? style
          : 'black',
    accent: a.rgb ?? s.fg,
    // Text on the accent: dark on light accents and on mono-in-dark, else white.
    onAccent: luminance(a.rgb ?? s.fg) > 0.6 ? [12, 12, 14] : [255, 255, 255],
    size: z,
  };
}

/** `rgb(r,g,b)` or `rgba(r,g,b,a)` for St CSS. */
export function css(rgb, alpha = 1) {
  const [r, g, b] = rgb.map((n) => Math.round(n));
  return alpha >= 1 ? `rgb(${r},${g},${b})` : `rgba(${r},${g},${b},${alpha})`;
}

/** 0-1 colour components for cairo. */
export function unit(rgb, alpha = 1) {
  return [rgb[0] / 255, rgb[1] / 255, rgb[2] / 255, alpha];
}

/** Relative luminance, 0 (black) to 1 (white). */
export function luminance([r, g, b]) {
  const f = (c) => {
    const v = c / 255;
    return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
}
