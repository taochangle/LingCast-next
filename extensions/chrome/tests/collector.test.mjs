import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createContext, runInContext } from 'node:vm';
import test from 'node:test';
import { parseHTML } from 'linkedom';

async function protocol() {
  return import('../protocol.js');
}

function fixture(platform) {
  const html = readFileSync(new URL(`./fixtures/${platform}.html`, import.meta.url), 'utf8');
  const { document } = parseHTML(html);
  const context = createContext({ document, crypto });
  for (const name of ['adapters.js', 'content.js']) {
    runInContext(readFileSync(new URL(`../${name}`, import.meta.url), 'utf8'), context);
  }
  return { document, ...context.LingCastAdapters, createQueue: context.LingCastContent.createQueue };
}

const data = value => JSON.parse(JSON.stringify(value));

test('room URLs must match the requested platform and an HTTPS live page', async () => {
  const { roomURL } = await protocol();
  assert.equal(roomURL('https://live.douyin.com/123?channel=foo', 'douyin'), 'https://live.douyin.com/123');
  assert.equal(roomURL('https://www.xiaohongshu.com/livestream/dynpath-a/456', 'xiaohongshu'),
    'https://www.xiaohongshu.com/livestream/456');
  for (const url of ['https://live.douyin.com.evil.test/123', 'https://www.douyin.com/video/123',
    'http://live.douyin.com/123', 'https://user@live.douyin.com/123', 'https://live.douyin.com/foo']) {
    assert.throws(() => roomURL(url, 'douyin'));
  }
  assert.throws(() => roomURL('https://live.douyin.com/123', 'xiaohongshu'));
});

test('Douyin keeps comment text literal and requires visible structured gift data', () => {
  const { document, douyin, xiaohongshu } = fixture('douyin');
  const rows = [...document.querySelectorAll(douyin.selector)];
  assert.equal(document.querySelectorAll(xiaohongshu.selector).length, 0);
  assert.deepEqual(data(rows.map(douyin.parse)), [
    { kind: 'comment', nickname: '<img onerror=alert(1)>', text: '送出 星星 x2' },
    { kind: 'gift', nickname: '小北', text: '星星 x2' },
    null,
    { kind: 'enter', nickname: '山风', text: '' },
    { kind: 'like', nickname: '晴天', text: '' },
  ]);
});

test('Xiaohongshu separately reads chat, gift, like and enter rows', () => {
  const { document, douyin, xiaohongshu } = fixture('xiaohongshu');
  const rows = [...document.querySelectorAll(xiaohongshu.selector)];
  assert.equal(document.querySelectorAll(douyin.selector).length, 0);
  assert.deepEqual(data(rows.map(xiaohongshu.parse)), [
    { kind: 'comment', nickname: '圆圆', text: '送出 星星 x2' },
    { kind: 'gift', nickname: '七月', text: '云朵 x3' },
    null,
    null,
    { kind: 'like', nickname: '茶客', text: '' },
    { kind: 'enter', nickname: '兰兰', text: '' },
  ]);
});

test('baseline DOM and duplicate updates are suppressed while new identical comments are retained', () => {
  const { document, douyin, createQueue } = fixture('douyin');
  const root = document.querySelector('.webcast-chatroom');
  const old = root.firstElementChild;
  const queue = createQueue(douyin);
  queue.baseline(document);
  queue.inspect(old);
  queue.inspect(old.cloneNode(true));
  assert.deepEqual(data(queue.drain()), []);
  const fresh = old.cloneNode(true);
  fresh.setAttribute('data-message-id', 'fresh');
  queue.inspect(fresh);
  queue.inspect(fresh);
  queue.inspect(fresh.cloneNode(true));
  const secondUserPost = fresh.cloneNode(true);
  secondUserPost.setAttribute('data-message-id', 'another');
  queue.inspect(secondUserPost);
  const events = data(queue.drain());
  assert.equal(events.length, 2);
  assert.notEqual(events[0].id, events[1].id);
  assert.equal(events[0].nickname, '<img onerror=alert(1)>');
});

test('queue drops oldest items during bursts and drains in bounded batches', () => {
  const { document, douyin, createQueue } = fixture('douyin');
  const row = document.querySelector(douyin.selector);
  const queue = createQueue(douyin);
  for (let n = 0; n < 150; n++) {
    const item = row.cloneNode(true);
    item.setAttribute('data-message-id', String(n));
    item.querySelector('.webcast-chatroom___display-text').lastElementChild.textContent = String(n);
    queue.inspect(item);
  }
  const first = data(queue.drain());
  assert.equal(first.length, 20);
  assert.equal(first[0].text, '50');
  assert.equal(queue.dropped, 50);
  assert.equal(Array.from({ length: 6 }, () => queue.drain().length).reduce((a, b) => a + b), 80);
});

test('binding rejects stale run, wrong tab, wrong frame, and cross-platform or different room', async () => {
  const { boundToRoom } = await protocol();
  const config = { tabId: 7, runId: 'live-run', platform: 'douyin', room: 'https://live.douyin.com/123' };
  const sender = { tab: { id: 7 }, frameId: 0, url: config.room };
  const message = { runId: config.runId, platform: config.platform, url: config.room };
  assert.equal(boundToRoom(config, message, sender), true);
  for (const changed of [
    { runId: 'old-run' }, { platform: 'xiaohongshu' }, { url: 'https://live.douyin.com/124' },
  ]) assert.equal(boundToRoom(config, { ...message, ...changed }, sender), false);
  assert.equal(boundToRoom(null, message, sender), false);
  assert.equal(boundToRoom(config, message, { ...sender, frameId: 1 }), false);
  assert.equal(boundToRoom(config, message, { ...sender, tab: { id: 8 } }), false);
  assert.equal(boundToRoom(config, message, { ...sender, url: 'https://live.douyin.com/124' }), false);
});

test('normalization returns only exact event fields and drops incomplete events', async () => {
  const { normalizeEvents } = await protocol();
  const nickname = '<img onerror=alert(1)>';
  assert.deepEqual(normalizeEvents([
    { id: ' a ', kind: 'comment', nickname, text: '<b>hello</b>', unwanted: 'field' },
    { id: 'b', kind: 'comment', nickname, text: ' ' },
    { id: 'c', kind: 'gift', nickname, text: '星星' },
    { id: 'd', kind: 'enter', nickname: '', text: '' },
    { id: 'e', kind: 'message', nickname, text: 'hello' },
  ]), [{ id: 'a', kind: 'comment', nickname, text: '<b>hello</b>' }]);
});

test('observer suppresses the first mounted history and stops reading after explicit stop', async () => {
  const platform = 'douyin';
  const html = readFileSync(new URL('./fixtures/douyin.html', import.meta.url), 'utf8');
  const { document, MutationObserver } = parseHTML(html);
  const chatroom = document.querySelector('#chatroom');
  chatroom.remove();
  const messages = [];
  const timers = new Map();
  let listener;
  let upload;
  let timerId = 0;
  const context = createContext({
    document, MutationObserver, crypto, location: { href: 'https://live.douyin.com/123' },
    setInterval: callback => { upload = callback; return 1; },
    clearInterval: () => { upload = null; },
    setTimeout: callback => { timers.set(++timerId, callback); return timerId; },
    clearTimeout: id => timers.delete(id),
    chrome: { runtime: {
      id: 'abc',
      onMessage: { addListener: handler => { listener = handler; } },
      sendMessage: async message => {
        messages.push(message);
        return { ok: true, processedIds: message.events.map(event => event.id) };
      },
    } },
  });
  for (const name of ['adapters.js', 'content.js']) {
    runInContext(readFileSync(new URL(`../${name}`, import.meta.url), 'utf8'), context);
  }
  const fromExtension = { id: 'abc' };
  listener({ type: 'LC_BEGIN', runId: 'active', platform }, fromExtension, reply => assert.equal(reply.ok, true));
  document.body.append(chatroom);
  await new Promise(resolve => setImmediate(resolve));
  await upload();
  assert.deepEqual(data(messages[0].events), []);
  assert.equal(messages[0].ready, true);

  const root = document.querySelector('.webcast-chatroom');
  const fresh = root.firstElementChild.cloneNode(true);
  fresh.setAttribute('data-message-id', 'fresh');
  root.append(fresh);
  await new Promise(resolve => setImmediate(resolve));
  for (const [id, callback] of timers) { timers.delete(id); callback(); }
  await upload();
  assert.equal(messages[1].events.length, 1);

  listener({ type: 'LC_STOP', runId: 'earlier' }, fromExtension, () => assert.fail('stale stop'));
  assert.equal(typeof upload, 'function');
  listener({ type: 'LC_STOP', runId: 'active' }, fromExtension, reply => assert.equal(reply.ok, true));
  root.append(fresh.cloneNode(true));
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(timers.size, 0);
  assert.equal(upload, null);
});

test('only reads the known XHS region, captures fast removal and attribute changes, and retries unresolved IDs', async () => {
  const { document, MutationObserver } = parseHTML('<html><body><section id="live-layout"></section></body></html>');
  const messages = [];
  let listener;
  let upload;
  let response;
  const context = createContext({
    document, MutationObserver, crypto, location: { href: 'https://www.xiaohongshu.com/livestream/1' },
    setInterval: callback => { upload = callback; return 1; },
    clearInterval: () => { upload = null; },
    setTimeout: callback => { setImmediate(callback); return 1; },
    clearTimeout: () => {},
    chrome: { runtime: {
      id: 'abc',
      onMessage: { addListener: handler => { listener = handler; } },
      sendMessage: async message => { messages.push(message); return response(message.events); },
    } },
  });
  for (const name of ['adapters.js', 'content.js']) {
    runInContext(readFileSync(new URL(`../${name}`, import.meta.url), 'utf8'), context);
  }
  listener({ type: 'LC_BEGIN', runId: 'active', platform: 'xiaohongshu' }, { id: 'abc' }, () => {});
  function row(text) {
    const element = document.createElement('div');
    element.innerHTML = '<span class="nickname">reader</span><span class="content"></span>';
    element.lastElementChild.textContent = text;
    return element;
  }
  const root = document.querySelector('#live-layout');
  const outsider = row('outside');
  outsider.className = 'msg-wrapper role-user';
  document.body.append(outsider);

  const quick = row('first');
  quick.className = 'msg-wrapper role-user';
  root.append(quick);
  quick.remove();

  const late = row('second');
  root.append(late);
  const settle = () => new Promise(resolve => setImmediate(resolve));
  await settle();
  late.className = 'msg-wrapper role-user';
  await settle();
  late.dataset.messageId = 'recognized-on-attribute';
  await settle();
  root.append(late.cloneNode(true));
  await settle();

  response = () => { throw new Error('Temporary runtime channel closure'); };
  await upload();
  assert.deepEqual([...messages[0].events.map(event => event.text)], ['first', 'second']);
  const [first, second] = messages[0].events;
  response = () => ({ ok: true, processedIds: [first.id] });
  await upload();
  assert.deepEqual([...messages[1].events.map(event => event.id)], [first.id, second.id]);
  response = () => ({ ok: true, processedIds: [second.id] });
  await upload();
  assert.deepEqual([...messages[2].events.map(event => event.id)], [second.id]);
  await upload();
  assert.deepEqual(data(messages[3].events), []);
  response = events => ({ ok: true, processedIds: events.map(event => event.id) });
  const hidden = document.createElement('div');
  const revealed = row('revealed');
  revealed.className = 'msg-wrapper role-user';
  hidden.hidden = true;
  hidden.append(revealed);
  const enter = document.createElement('div');
  enter.innerHTML = '<div class="msg-content"><span class="nickname">visitor</span><span>来了</span></div>';
  root.append(hidden, enter);
  await settle();
  await upload();
  assert.deepEqual(data(messages.at(-1).events), []);

  hidden.hidden = false;
  enter.className = 'enter-msgs';
  await settle();
  await upload();
  assert.deepEqual(data(messages.at(-1).events.map(({ kind, nickname, text }) => ({ kind, nickname, text }))), [
    { kind: 'comment', nickname: 'reader', text: 'revealed' },
    { kind: 'enter', nickname: 'visitor', text: '' },
  ]);
  hidden.className = 'updated-wrapper';
  enter.classList.add('updated-wrapper');
  await settle();
  await upload();
  assert.deepEqual(data(messages.at(-1).events), []);

  response = () => ({ ok: false, stop: true });
  await upload();
  assert.equal(upload, null);

  root.remove();
  listener({ type: 'LC_BEGIN', runId: 'next', platform: 'xiaohongshu' }, { id: 'abc' }, () => {});
  response = () => ({ ok: true, processedIds: [] });
  await upload();
  assert.equal(messages.at(-1).ready, false);
  assert.deepEqual(data(messages.at(-1).events), []);
});
