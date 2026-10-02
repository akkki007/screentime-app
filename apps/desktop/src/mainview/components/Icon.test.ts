import { expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import path from 'node:path';

test('Icon component defines globe icon', () => {
  const iconPath = path.join(__dirname, 'Icon.svelte');
  const content = readFileSync(iconPath, 'utf8');
  expect(content).toContain("'globe'");
  expect(content).toContain('globe:');
});

test('LimitsView renders globe icon for domain limits', () => {
  const limitsViewPath = path.join(__dirname, '../views/LimitsView.svelte');
  const content = readFileSync(limitsViewPath, 'utf8');
  expect(content).toContain('limit.targetType === "category" ? "apps" : "globe"');
});
