import { boundToRoom, normalizeEvents, roomURL, serverOrigin } from './protocol.js';

const stateKey = 'collector';
const staleContactMs = 40_000;
let transitions = Promise.resolve();
let upload;

function transition(action) {
  const result = transitions.then(action);
  transitions = result.catch(() => {});
  return result;
}

async function state() {
  return (await chrome.storage.session.get(stateKey))[stateKey];
}

function save(collector) {
  return chrome.storage.session.set({ [stateKey]: collector });
}

async function request(config, events, signal = AbortSignal.timeout(8_000)) {
  let response;
  try {
    response = await fetch(config.server + '/api/collector/events', {
      method: 'POST',
      credentials: 'omit',
      redirect: 'error',
      headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${config.token}` },
      body: JSON.stringify({ sessionId: config.sessionId, platform: config.platform, events }),
      signal,
    });
  } catch {
    throw new Error('无法连接 LingCast 服务器，请检查地址和网络');
  }
  if (!response.ok) {
    const messages = {
      400: '服务器拒绝采集请求，请核对直播平台和会话配置',
      401: '采集密钥已失效，请从当前直播会话复制新密钥',
      403: '服务器拒绝访问采集接口',
      409: '直播会话已无法接收采集事件',
    };
    const error = new Error(messages[response.status] || `采集服务器返回 HTTP ${response.status}`);
    error.status = response.status;
    throw error;
  }
  const result = await response.json().catch(() => null);
  const { accepted, processedIds, retryIds } = result ?? {};
  if (!Number.isSafeInteger(accepted) || accepted < 0 || !Array.isArray(processedIds) ||
    !Array.isArray(retryIds) || accepted > processedIds.length ||
    processedIds.length + retryIds.length !== events.length) {
    throw new Error('服务器响应与 LingCast 采集接口不符');
  }
  const remaining = new Set(events.map(event => event.id));
  for (const id of [...processedIds, ...retryIds]) {
    if (!remaining.delete(id)) throw new Error('服务器返回的互动确认编号无效');
  }
  return { accepted, processedIds, retryIds };
}

function sendToCollector(current, type, extra = {}) {
  const target = current.documentId ? { documentId: current.documentId } : { frameId: 0 };
  return chrome.tabs.sendMessage(current.tabId, { type, runId: current.runId, ...extra }, target);
}

async function stop(current, message = '已停止采集') {
  await save({ message, accepted: current?.accepted ?? 0, discarded: current?.discarded ?? 0 });
  if (!current?.runId) return;
  if (upload?.runId === current.runId) upload.controller.abort();
  sendToCollector(current, 'LC_STOP').catch(() => {});
}

async function start(message) {
  const server = serverOrigin(message.server);
  const { sessionId, token, platform, tabId } = message;
  if (!/^[a-f\d]{8}(?:-[a-f\d]{4}){3}-[a-f\d]{12}$/i.test(sessionId)) throw new Error('请填写直播会话 ID');
  if (typeof token !== 'string' || !/^[A-Za-z0-9_-]{1,256}$/.test(token)) throw new Error('请填写直播采集密钥');
  if (!Number.isInteger(tabId)) throw new Error('请打开当前直播标签页');
  const tab = await chrome.tabs.get(tabId);
  const room = roomURL(tab.url, platform);
  if (!await chrome.permissions.contains({ origins: [server + '/*'] })) throw new Error('请授权访问 LingCast 服务器');

  const config = { server, sessionId, token, platform, tabId, room, runId: crypto.randomUUID() };
  await request(config, []);
  const previous = await state();
  if (previous?.runId) await stop(previous);
  try {
    const [injection] = await chrome.scripting.executeScript({
      target: { tabId },
      files: ['adapters.js', 'content.js'],
    });
    const latestTab = await chrome.tabs.get(tabId);
    if (roomURL(latestTab.url, platform) !== room) throw new Error('直播页面已切换');
    config.documentId = injection.documentId;
    await save({ ...config, accepted: 0, discarded: 0, message: '正在识别直播互动区域', lastContact: Date.now() });
    const reply = await sendToCollector(config, 'LC_BEGIN', { platform });
    if (!reply?.ok) throw new Error('页面未响应');
  } catch {
    await stop(config, '无法读取当前直播页面，请重新打开直播间');
    throw new Error('无法读取当前直播页面，请重新打开直播间');
  }
  return { ok: true };
}

async function prepareUpload(message, sender) {
  const current = await state();
  if (!boundToRoom(current, message, sender)) {
    if (current?.runId === message.runId && current.tabId === sender.tab?.id && sender.frameId === 0) {
      await stop(current, '直播页面已切换，请重新开始采集');
    }
    return { reply: { ok: false, stop: true } };
  }

  const now = Date.now();
  if (now - current.lastContact >= 5_000) {
    current.lastContact = now;
    await save(current);
  }
  const events = normalizeEvents(message.events);
  const statusMessage = message.ready ? '正在监听直播互动' : '等待直播互动区域加载';
  const discarded = Number.isSafeInteger(message.discarded) && message.discarded >= 0 ? message.discarded : 0;
  if (!events.length) {
    if (current.message !== statusMessage || current.discarded !== discarded) {
      await save({ ...current, discarded, message: statusMessage });
    }
    return { reply: { ok: true, processedIds: [] } };
  }
  if (upload?.runId === current.runId) return { reply: { ok: false } };
  const task = { runId: current.runId, controller: new AbortController() };
  upload = task;
  return { current, events, statusMessage, discarded, task };
}

async function receiveEvents(message, sender) {
  const { reply, current, events, statusMessage, discarded, task } = await transition(() => prepareUpload(message, sender));
  if (reply) return reply;
  try {
    const signal = AbortSignal.any([task.controller.signal, AbortSignal.timeout(25_000)]);
    const { accepted, processedIds, retryIds } = await request(current, events, signal);
    return await transition(async () => {
      const latest = await state();
      if (latest?.runId !== current.runId) return { ok: false, stop: true };
      const message = retryIds.length ? '正在重试待确认互动' : statusMessage;
      await save({ ...latest, accepted: latest.accepted + accepted, discarded, message });
      return { ok: true, processedIds };
    });
  } catch (error) {
    return await transition(async () => {
      const latest = await state();
      if (latest?.runId !== current.runId) return { ok: false, stop: true };
      if ([400, 401, 403, 404, 405, 409].includes(error.status)) {
        await stop(latest, error.message);
        return { ok: false, stop: true };
      }
      await save({ ...latest, discarded, message: error.message });
      return { ok: false };
    });
  } finally {
    if (upload === task) upload = undefined;
  }
}

async function collectorStatus() {
  let current = await state();
  if (current?.runId && Date.now() - current.lastContact >= staleContactMs) {
    const reply = await sendToCollector(current, 'LC_PING').catch(() => null);
    if (!reply?.ok) {
      await stop(current, '直播页面连接已断开，请重新开始采集');
      current = await state();
    } else {
      current.lastContact = Date.now();
      await save(current);
    }
  }
  return { ok: true, status: {
    active: Boolean(current?.runId),
    accepted: current?.accepted ?? 0,
    discarded: current?.discarded ?? 0,
    message: current?.message ?? '采集未启动',
  } };
}

async function handle(message, sender) {
  if (sender.id !== chrome.runtime.id || !message) return { ok: false };
  if (message.type === 'LC_EVENTS') return receiveEvents(message, sender);
  if (sender.tab || sender.url !== chrome.runtime.getURL('popup.html')) return { ok: false };
  if (message.type === 'LC_START') return transition(() => start(message));
  if (message.type === 'LC_STOP') {
    return transition(async () => {
      await stop(await state());
      return { ok: true };
    });
  }
  if (message.type === 'LC_STATUS') return transition(collectorStatus);
  return { ok: false };
}

chrome.runtime.onMessage.addListener((message, sender, respond) => {
  handle(message, sender).then(respond, error => respond({ ok: false, error: error.message }));
  return true;
});

function stopForTab(tabId, message) {
  transition(async () => {
    const current = await state();
    if (current?.runId && current.tabId === tabId) await stop(current, message);
  }).catch(() => {});
}

chrome.tabs.onRemoved.addListener(tabId => stopForTab(tabId, '直播标签页已关闭'));
chrome.tabs.onUpdated.addListener((tabId, change) => {
  if (change.status === 'loading') stopForTab(tabId, '直播页面已刷新，请重新开始采集');
});
