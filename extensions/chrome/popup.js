import { roomURL, serverOrigin } from './protocol.js';

const form = document.querySelector('#settings');
const server = document.querySelector('#server');
const sessionId = document.querySelector('#session-id');
const token = document.querySelector('#token');
const platform = document.querySelector('#platform');
const start = document.querySelector('#start');
const stop = document.querySelector('#stop');
const status = document.querySelector('#status');
const error = document.querySelector('#error');
let busy = false;
let active = false;

function updateControls() {
  start.disabled = busy || active;
  stop.disabled = busy || !active;
  for (const field of [server, sessionId, token, platform]) field.disabled = busy || active;
}

function showError(message = '') {
  error.textContent = message;
  error.hidden = !message;
}

async function refreshStatus() {
  const result = await chrome.runtime.sendMessage({ type: 'LC_STATUS' });
  if (!result?.ok) return;
  const next = result.status;
  active = next.active;
  const details = [next.message];
  if (active || next.accepted > 0) details.push(`已接收 ${next.accepted} 条`);
  if (next.discarded > 0) details.push(`超时或拥堵移出队列 ${next.discarded} 条`);
  status.textContent = details.join(' · ');
  updateControls();
}

async function action(callback) {
  busy = true;
  updateControls();
  showError();
  try {
    await callback();
  } catch (error) {
    showError(error.message);
  } finally {
    busy = false;
    updateControls();
    refreshStatus().catch(() => {});
  }
}

form.addEventListener('submit', event => {
  event.preventDefault();
  const selected = { sessionId: sessionId.value.trim(), token: token.value.trim(), platform: platform.value };
  action(async () => {
    const origin = serverOrigin(server.value.trim());
    // The optional site permission request must begin in the submit gesture.
    if (!await chrome.permissions.request({ origins: [origin + '/*'] })) throw new Error('请授权访问 LingCast 服务器');
    const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
    roomURL(tab?.url, selected.platform);
    const result = await chrome.runtime.sendMessage({ type: 'LC_START', ...selected, server: origin, tabId: tab.id });
    if (!result?.ok) throw new Error(result?.error || '无法启动采集');
    active = true;
    token.value = '';
    await chrome.storage.local.set({ collectorPrefs: { server: origin, sessionId: selected.sessionId, platform: selected.platform } });
  });
});

stop.addEventListener('click', () => {
  action(async () => {
    const result = await chrome.runtime.sendMessage({ type: 'LC_STOP' });
    if (!result?.ok) throw new Error(result?.error || '无法停止采集');
    active = false;
  });
});

async function initialize() {
  const { collectorPrefs } = await chrome.storage.local.get('collectorPrefs');
  if (collectorPrefs) {
    server.value = collectorPrefs.server;
    sessionId.value = collectorPrefs.sessionId;
    platform.value = collectorPrefs.platform;
  }
  await refreshStatus();
}

initialize().catch(error => showError(error.message));
setInterval(() => {
  if (!busy) refreshStatus().catch(() => {});
}, 1000);
