// The Shell's notch model must pass the same fixtures as internal/notch.
import { describe, expect, test } from 'bun:test';
import fixtures from '../../internal/notch/testdata/fixtures.json';
import { NotchModel, countdown, formatShort } from './notch.js';

test('formatShort matches the dashboard', () => {
  for (const [ms, want] of fixtures.formatShort) expect(formatShort(ms)).toBe(want);
});

test('countdown', () => {
  for (const [ms, want] of fixtures.countdown) expect(countdown(ms)).toBe(want);
});

describe('fixtures', () => {
  for (const c of fixtures.cases) {
    test(c.name, () => {
      const m = new NotchModel(c.opts);
      for (const [i, s] of c.steps.entries()) {
        const at = s.at;
        if ('connected' in s) m.setConnected(s.connected);
        else if ('status' in s) m.setStatus(s.status, at);
        else if ('today' in s) m.setToday(s.today);
        else if ('limitHit' in s) m.limitHit(s.limitHit.target, s.limitHit.dailyMs, at);
        else if ('reminder' in s) m.reminder(s.reminder.kind, s.reminder.message, at);
        else if ('expect' in s)
          expect({ step: i, view: m.view(at) }).toEqual({ step: i, view: s.expect });
        else if ('expectPanel' in s)
          expect({ step: i, panel: m.panel() }).toEqual({ step: i, panel: s.expectPanel });
        else if ('expectNext' in s)
          expect({ step: i, next: m.nextChange(at) }).toEqual({ step: i, next: s.expectNext });
        else throw new Error(`step ${i} does nothing`);
      }
    });
  }
});
