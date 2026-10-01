/**
 * Tracks the active tab's domain and forwards `{domain, active}` to the
 * native messaging host (native-host/host.ts), which relays it to the
 * daemon's Unix socket. The extension never talks to the socket directly —
 * browsers can't open arbitrary Unix sockets, hence native messaging.
 *
 * Only the hostname is sent, and private/incognito tabs are never reported.
 */
import { domainOf } from './domain';

const NATIVE_HOST_NAME = 'io.github.akkki007.screentime';

let port: chrome.runtime.Port | undefined;
let lastReported: string | undefined;

function connectNativeHost() {
  port = chrome.runtime.connectNative(NATIVE_HOST_NAME);
  port.onDisconnect.addListener(() => {
    // Read lastError so the browser doesn't log "unchecked" when the host
    // isn't installed or the daemon is down; we retry on the next report.
    void chrome.runtime.lastError;
    port = undefined;
    lastReported = undefined;
  });
}

function send(message: { domain?: string; active: boolean }) {
  if (!port) connectNativeHost();
  port?.postMessage(message);
}

function reportActiveTab(tab: chrome.tabs.Tab | undefined) {
  if (!tab || tab.incognito) return;
  const domain = domainOf(tab.url);
  if (!domain) {
    // Switched to a non-web page (new tab, settings): stop attributing time.
    if (lastReported !== undefined) {
      lastReported = undefined;
      send({ active: false });
    }
    return;
  }
  if (domain === lastReported) return;
  lastReported = domain;
  send({ domain, active: true });
}

chrome.tabs.onActivated.addListener(({ tabId }) => {
  chrome.tabs.get(tabId, reportActiveTab);
});

chrome.tabs.onUpdated.addListener((_tabId, changeInfo, tab) => {
  if (changeInfo.status === 'complete' && tab.active) reportActiveTab(tab);
});

chrome.windows.onFocusChanged.addListener((windowId) => {
  if (windowId === chrome.windows.WINDOW_ID_NONE) {
    lastReported = undefined;
    send({ active: false });
    return;
  }
  chrome.tabs.query({ active: true, windowId }, ([tab]) => reportActiveTab(tab));
});
