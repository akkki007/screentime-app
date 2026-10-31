import { describe, expect, test } from 'bun:test';
import { ACCENTS, STYLES, css, styleFromBar, theme, unit } from './palette.js';

describe('theme', () => {
  test('matches the top bar by default, and falls back to black without one', () => {
    expect(theme().bg).toEqual([0, 0, 0]);
    expect(theme({ bar: [19, 19, 19, 255] }).bg).toEqual([19, 19, 19]);
  });

  test('a see-through bar has nothing to match, so the island is black', () => {
    expect(styleFromBar([40, 40, 40, 120]).bg).toEqual([0, 0, 0]);
  });

  test('a light bar gets a light island with dark text', () => {
    const t = theme({ bar: [246, 245, 244, 255] });
    expect(t.name).toBe('light');
    expect(t.fg).toEqual(STYLES.light.fg);
    expect(t.bg).toEqual([246, 245, 244]);
  });

  test('black style is plain black', () => {
    const t = theme({ style: 'black' });
    expect(t.name).toBe('black');
    expect(t.bg).toEqual([0, 0, 0]);
    expect(t.accent).toEqual(ACCENTS.blue.rgb);
    expect(t.size.height).toBe(34);
  });

  test('unknown values fall back instead of breaking the island', () => {
    const t = theme({ style: 'neon', accent: 'gold', size: 'huge' });
    expect(t.name).toBe('black');
    expect(t.accent).toEqual(ACCENTS.blue.rgb);
    expect(t.size.height).toBe(34);
  });

  test('mono uses the style text colour, with readable text on it', () => {
    expect(theme({ style: 'black', accent: 'mono' }).accent).toEqual(STYLES.black.fg);
    expect(theme({ style: 'black', accent: 'mono' }).onAccent).toEqual([12, 12, 14]);
    expect(theme({ style: 'light', accent: 'mono' }).onAccent).toEqual([255, 255, 255]);
  });

  test('every style keeps its text readable on its background', () => {
    for (const s of Object.values(STYLES)) {
      const contrast = (a, b) => {
        const l = (c) => {
          const f = (v) => {
            const x = v / 255;
            return x <= 0.03928 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4;
          };
          return 0.2126 * f(c[0]) + 0.7152 * f(c[1]) + 0.0722 * f(c[2]);
        };
        const [hi, lo] = [l(a), l(b)].sort((x, y) => y - x);
        return (hi + 0.05) / (lo + 0.05);
      };
      expect(contrast(s.fg, s.bg)).toBeGreaterThan(7);
      expect(contrast(s.muted, s.bg)).toBeGreaterThan(4.5);
      expect(contrast(s.muted, s.card)).toBeGreaterThan(4.5);
    }
  });
});

test('css and unit', () => {
  expect(css([1, 2, 3])).toBe('rgb(1,2,3)');
  expect(css([1, 2, 3], 0.5)).toBe('rgba(1,2,3,0.5)');
  expect(unit([255, 0, 51], 0.5)).toEqual([1, 0, 0.2, 0.5]);
});
