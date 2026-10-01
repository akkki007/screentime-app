import type { ElectrobunConfig } from 'electrobun';

export default {
  app: {
    name: 'Screentime',
    identifier: 'io.github.akkki007.screentime',
    version: '0.1.0',
  },
  build: {
    // Bun main process: the UI talks to the daemon over a Unix socket with
    // Bun.connect, exactly like the daemon's own tooling.
    mainProcess: 'bun',
    bun: {
      entrypoint: 'src/bun/index.ts',
    },
    // Vite builds the Svelte UI to dist/; copy it into the app bundle.
    copy: {
      'dist/index.html': 'views/mainview/index.html',
      'dist/assets': 'views/mainview/assets',
      'src/assets/tray.png': 'views/assets/tray.png',
      'src/assets/tray-paused.png': 'views/assets/tray-paused.png',
      'src/assets/tray-focus.png': 'views/assets/tray-focus.png',
      'src/assets/tray-offline.png': 'views/assets/tray-offline.png',
    },
    watchIgnore: ['dist/**'],
    mac: { bundleCEF: false },
    linux: { bundleCEF: false },
    win: { bundleCEF: false },
  },
  // Closing the window leaves the UI in the tray; tracking never depended on it.
  runtime: {
    exitOnLastWindowClosed: false,
  },
} satisfies ElectrobunConfig;
