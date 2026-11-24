import assert from 'node:assert/strict';
import test from 'node:test';

test('background authorizes the binding, forwards only new event fields, and rejects stale uploads', async () => {
  const tabId = 19;
  const url = 'https://live.douyin.com/321';
  const sessionId = 'cff91be5-408d-4cc6-97bd-0ab6c8a2fd5a';
  const token = 'synthetic-test-token';
  const store = {};
  const calls = [];
  const tabMessages = [];
  let messageListener;
  let serverStatus = 200;
  globalThis.chrome = {
    runtime: {
      id: 'abc',
      getURL: path => `chrome-extension://abc/${path}`,
      onMessage: { addListener: handler => { messageListener = handler; } },
    },
    storage: {
      session: {
        get: async key => ({ [key]: structuredClone(store[key]) }),
        set: async patch => Object.assign(store, structuredClone(patch)),
      },
    },
    tabs: {
      get: async () => ({ id: tabId, url }),
      onRemoved: { addListener() {} },
      onUpdated: { addListener() {} },
      sendMessage: async (id, message) => {
        assert.equal(id, tabId);
        tabMessages.push(message);
        return { ok: true };
      },
    },
    scripting: { executeScript: async () => [{ frameId: 0 }] },
    permissions: { contains: async () => true },
  };
  const realFetch = globalThis.fetch;
  globalThis.fetch = async (endpoint, options) => {
    const body = JSON.parse(options.body);
    calls.push({ endpoint, body, headers: options.headers, credentials: options.credentials, redirect: options.redirect });
    return new Response(JSON.stringify({ accepted: body.events.length, serverTime: '2026-11-24T12:00:00Z' }),
      { status: serverStatus });
  };
  const popup = { id: 'abc', url: 'chrome-extension://abc/popup.html' };
  const page = { id: 'abc', tab: { id: tabId }, frameId: 0, url };
  const send = (message, sender = popup) => new Promise(resolve => messageListener(message, sender, resolve));
  const config = { type: 'LC_START', server: 'http://localhost:8080', sessionId, token, platform: 'douyin', tabId };

  try {
    await import('../background.js');
    assert.equal((await send(config, page)).ok, false);
    assert.equal((await send({ ...config, platform: 'xiaohongshu' })).ok, false);
    assert.equal(calls.length, 0);
    assert.equal((await send(config)).ok, true);
    const { runId } = tabMessages[0];
    assert.deepEqual(tabMessages[0], { type: 'LC_BEGIN', runId, platform: 'douyin' });
    assert.deepEqual(calls[0], {
      endpoint: 'http://localhost:8080/api/collector/events',
      body: { sessionId, platform: 'douyin', events: [] },
      headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
      credentials: 'omit', redirect: 'error',
    });
    const payload = {
      type: 'LC_EVENTS', runId, platform: 'douyin', url, ready: true,
      events: [{ id: 'sample', kind: 'comment', nickname: '<img>', text: 'hello', unwanted: 1 }],
    };
    assert.equal((await send({ ...payload, runId: 'stale' }, page)).stop, true);
    assert.equal(calls.length, 1);
    assert.equal((await send(payload, page)).ok, true);
    assert.deepEqual(calls[1].body.events, [{ id: 'sample', kind: 'comment', nickname: '<img>', text: 'hello' }]);
    const status = await send({ type: 'LC_STATUS' });
    assert.equal(status.status.accepted, 1);
    assert.equal(JSON.stringify(status).includes(token), false);
    assert.equal((await send({ type: 'LC_STOP' })).ok, true);
    assert.equal((await send(payload, page)).stop, true);
    assert.equal(calls.length, 2);
    assert.equal(JSON.stringify(store).includes(token), false);

    serverStatus = 401;
    assert.equal((await send(config)).ok, false);
    assert.equal(tabMessages.filter(message => message.type === 'LC_BEGIN').length, 1);
  } finally {
    globalThis.fetch = realFetch;
    delete globalThis.chrome;
  }
});
