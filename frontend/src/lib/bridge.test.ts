import { describe, expect, test } from 'bun:test';
import { waitForApp } from './bridge';

describe('waitForApp', () => {
  test('a plain browser is not the app, and answers at once', async () => {
    const start = Date.now();
    expect(await waitForApp({})).toBe(false);
    expect(Date.now() - start).toBeLessThan(50);
  });

  test('the environment already set is the app', async () => {
    expect(await waitForApp({ _wails: { environment: {} } })).toBe(true);
  });

  test('inside WebKitGTK, waits for the environment set after load', async () => {
    const win: Parameters<typeof waitForApp>[0] = { webkit: { messageHandlers: { external: {} } } };
    setTimeout(() => {
      win._wails = { environment: {} };
    }, 40);
    expect(await waitForApp(win, { timeoutMs: 1000, intervalMs: 5 })).toBe(true);
  });

  test('inside WebView2, waits the same way', async () => {
    const win: Parameters<typeof waitForApp>[0] = { chrome: { webview: {} } };
    setTimeout(() => {
      win._wails = { environment: {} };
    }, 40);
    expect(await waitForApp(win, { timeoutMs: 1000, intervalMs: 5 })).toBe(true);
  });

  test('gives up if the environment never comes', async () => {
    const win = { webkit: { messageHandlers: { external: {} } } };
    expect(await waitForApp(win, { timeoutMs: 60, intervalMs: 5 })).toBe(false);
  });
});
