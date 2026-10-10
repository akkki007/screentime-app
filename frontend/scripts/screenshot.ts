/**
 * Screenshots a page of the UI by driving an isolated headless Firefox over
 * WebDriver BiDi. Used to regenerate docs/media and to eyeball changes without
 * a display. It captures only the page, never your desktop.
 *
 *   bun run screenshot -- <out.png> <url> [WIDTHxHEIGHT] [waitMs] [click...]
 *
 * Start the UI first (`bun run hmr`) and use the mock-data preview, e.g.
 *   bun run screenshot -- /tmp/limits.png "http://localhost:5173/?view=limits" 1280x700
 *
 * Each `click` is a CSS selector, or `text=Some label` to click the first
 * button containing that text. It runs in its own Firefox profile (never yours);
 * a snap-packaged Firefox can only use a non-hidden directory under $HOME, so
 * that is where the profile goes (override with SHOT_PROFILE).
 */
import { spawn } from 'node:child_process';
import { mkdirSync, writeFileSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';

const [out, url, size = '1280x1000', wait = '1500', ...clicks] = process.argv.slice(2);
if (!out || !url) {
  console.error('usage: screenshot <out.png> <url> [WIDTHxHEIGHT] [waitMs] [click...]');
  process.exit(2);
}

const [width, height] = size.split('x').map(Number);
const profile = process.env.SHOT_PROFILE ?? join(homedir(), 'screentime-shots-profile');
mkdirSync(profile, { recursive: true });
const port = 9300 + Math.floor(Math.random() * 500);

const firefox = spawn(
  'firefox',
  [
    '--headless',
    '--no-remote',
    '--profile',
    profile,
    `--remote-debugging-port=${port}`,
    'about:blank',
  ],
  { stdio: 'ignore' },
);
const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

async function connect(): Promise<WebSocket> {
  for (let attempt = 0; attempt < 60; attempt++) {
    try {
      const socket = new WebSocket(`ws://127.0.0.1:${port}/session`);
      await new Promise<void>((resolve, reject) => {
        socket.onopen = () => resolve();
        socket.onerror = () => reject(new Error('not ready'));
      });
      return socket;
    } catch {
      await sleep(250);
    }
  }
  throw new Error('could not reach Firefox over WebDriver BiDi (is firefox installed?)');
}

type BidiReply = { id: number; type?: string; message?: string; result?: Record<string, unknown> };

const socket = await connect();
let nextId = 0;
const waiting = new Map<number, (reply: BidiReply) => void>();
socket.onmessage = (event) => {
  const reply = JSON.parse(String(event.data)) as BidiReply;
  waiting.get(reply.id)?.(reply);
};

function send(method: string, params: object = {}): Promise<Record<string, unknown>> {
  return new Promise((resolve, reject) => {
    const id = ++nextId;
    waiting.set(id, (reply) =>
      reply.type === 'error'
        ? reject(new Error(`${method}: ${reply.message}`))
        : resolve(reply.result ?? {}),
    );
    socket.send(JSON.stringify({ id, method, params }));
  });
}

try {
  await send('session.new', { capabilities: {} });
  const tree = (await send('browsingContext.getTree')) as { contexts: { context: string }[] };
  const context = tree.contexts[0]?.context;
  if (!context) throw new Error('no browsing context');

  await send('browsingContext.setViewport', { context, viewport: { width, height } });
  await send('browsingContext.navigate', { context, url, wait: 'complete' });
  await sleep(Number(wait)); // the UI loads its data asynchronously after the load event

  for (const selector of clicks) {
    const expression = `(() => {
      const sel = ${JSON.stringify(selector)};
      const el = sel.startsWith('text=')
        ? [...document.querySelectorAll('button')].find((b) => b.textContent.trim().includes(sel.slice(5)))
        : document.querySelector(sel);
      if (!el) return false;
      el.click();
      return true;
    })()`;
    const result = (await send('script.evaluate', {
      expression,
      target: { context },
      awaitPromise: false,
    })) as {
      result?: { value?: boolean };
    };
    if (result.result?.value !== true) console.error(`click target not found: ${selector}`);
    await sleep(900);
  }

  const shot = (await send('browsingContext.captureScreenshot', { context })) as { data: string };
  writeFileSync(out, Buffer.from(shot.data, 'base64'));
  console.log(`saved ${out}`);
} finally {
  socket.close();
  firefox.kill();
}
