import { expect, test } from 'bun:test';
import { selectFocusProvider } from './index';

test('SCREENTIME_FOCUS_PROVIDER forces an adapter by id', async () => {
  expect((await selectFocusProvider('none')).id).toBe('none');
  expect((await selectFocusProvider('x11')).id).toBe('x11');
});

test('an unknown or inherited id is rejected', async () => {
  await expect(selectFocusProvider('wayland')).rejects.toThrow('unknown SCREENTIME_FOCUS_PROVIDER');
  await expect(selectFocusProvider('constructor')).rejects.toThrow('unknown');
});

test('the none adapter reports nothing', async () => {
  const provider = await selectFocusProvider('none');
  expect(await provider.isAvailable()).toBe(true);
  const unsubscribe = provider.onFocusChange(() => {
    throw new Error('should never fire');
  });
  unsubscribe();
});
