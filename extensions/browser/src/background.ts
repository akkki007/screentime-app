/**
 * Tracks the active tab's domain and forwards `{domain, active}` to the
 * native messaging host (native-host/host.ts), which relays it to the
 * daemon's Unix socket. The extension never talks to the socket directly —
 * browsers can't open arbitrary Unix sockets, hence native messaging.
 */
const NATIVE_HOST_NAME = 'io.github.akkki007.screentime';

let port: chrome.runtime.Port | undefined;

function connectNativeHost() {
  port = chrome.runtime.connectNative(NATIVE_HOST_NAME);
  port.onDisconnect.addListener(() => {
    port = undefined;
  });
}

function domainOf(url: string | undefined): string | undefined {
  if (!url) return undefined;
  try {
    return new URL(url).hostname || undefined;
  } catch {
    return undefined;
  }
}

function reportActiveTab(tab: chrome.tabs.Tab | undefined) {
  const domain = domainOf(tab?.url);
  if (!domain) return;
  if (!port) connectNativeHost();
  port?.postMessage({ domain, active: true });
}

chrome.tabs.onActivated.addListener(({ tabId }) => {
  chrome.tabs.get(tabId, reportActiveTab);
});

chrome.tabs.onUpdated.addListener((tabId, changeInfo, tab) => {
  if (changeInfo.status === 'complete') reportActiveTab(tab);
});

chrome.windows.onFocusChanged.addListener((windowId) => {
  if (windowId === chrome.windows.WINDOW_ID_NONE) {
    port?.postMessage({ domain: undefined, active: false });
    return;
  }
  chrome.tabs.query({ active: true, windowId }, ([tab]) => reportActiveTab(tab));
});
