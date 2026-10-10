import { describe, expect, test } from 'bun:test';
import { buildHostManifest, buildManifest, hostManifestPath } from './lib';

const base = {
  manifest_version: 3,
  name: 'Screentime',
  permissions: ['tabs', 'nativeMessaging'],
  browser_specific_settings: { gecko: { id: 'screentime@akkki007.github.io' } },
};

describe('buildManifest', () => {
  test('chromium gets a service worker and no gecko settings', () => {
    const m = buildManifest(base, 'chromium');
    expect(m.background).toEqual({ service_worker: 'background.js', type: 'module' });
    expect(m).not.toHaveProperty('browser_specific_settings');
  });

  test('firefox gets background scripts and keeps its extension id', () => {
    const m = buildManifest(base, 'firefox');
    expect(m.background).toEqual({ scripts: ['background.js'], type: 'module' });
    expect(m.browser_specific_settings).toEqual(base.browser_specific_settings);
  });

  test('does not mutate the base manifest', () => {
    buildManifest(base, 'chromium');
    expect(base).toHaveProperty('browser_specific_settings');
    expect(base).not.toHaveProperty('background');
  });
});

describe('native host manifests', () => {
  test('locations per browser', () => {
    expect(hostManifestPath('firefox', '/h')).toBe(
      '/h/.mozilla/native-messaging-hosts/io.github.akkki007.screentime.json',
    );
    expect(hostManifestPath('chrome', '/h')).toBe(
      '/h/.config/google-chrome/NativeMessagingHosts/io.github.akkki007.screentime.json',
    );
  });

  test('firefox restricts to our extension id', () => {
    const m = buildHostManifest('firefox', '/x/host');
    expect(m.allowed_extensions).toEqual(['screentime@akkki007.github.io']);
    expect(m.path).toBe('/x/host');
  });

  test('chromium restricts to one origin and rejects a missing or malformed id', () => {
    const id = 'a'.repeat(32);
    expect(buildHostManifest('chrome', '/x/host', id).allowed_origins).toEqual([
      `chrome-extension://${id}/`,
    ]);
    expect(() => buildHostManifest('chrome', '/x/host')).toThrow('Chromium extension ID');
    expect(() => buildHostManifest('chrome', '/x/host', 'short')).toThrow('Chromium extension ID');
    expect(() => buildHostManifest('chrome', '/x/host', `${'a'.repeat(31)}z`)).toThrow(
      'Chromium extension ID',
    );
  });
});
