export const BATCH_LIMIT = 20;

export function serverOrigin(raw) {
  let url;
  try {
    url = new URL(raw);
  } catch {
    throw new Error('请填写有效的 LingCast 服务器地址');
  }
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password ||
    url.pathname !== '/' || url.search || url.hash) {
    throw new Error('服务器地址只填写 HTTP(S) 主机和端口');
  }
  return url.origin;
}

export function roomURL(raw, platform) {
  const mismatch = new Error('请打开所选平台的 HTTPS 直播间');
  let url;
  try {
    url = new URL(raw);
  } catch {
    throw mismatch;
  }
  if (url.protocol !== 'https:' || url.port || url.username || url.password) throw mismatch;

  if (platform === 'douyin' && url.hostname === 'live.douyin.com' && /^\/\d+\/?$/.test(url.pathname)) {
    return url.origin + url.pathname.replace(/\/$/, '');
  }
  if (platform === 'xiaohongshu' && ['xiaohongshu.com', 'www.xiaohongshu.com'].includes(url.hostname)) {
    const match = url.pathname.match(/^\/livestream\/(?:dynpath[A-Za-z0-9_-]+\/)?(\d+)\/?$/);
    if (match) return `https://www.xiaohongshu.com/livestream/${match[1]}`;
  }
  throw mismatch;
}

export function boundToRoom(config, message, sender) {
  if (!config || sender.tab?.id !== config.tabId || sender.frameId !== 0 ||
    message.runId !== config.runId || message.platform !== config.platform ||
    (config.documentId && sender.documentId !== config.documentId)) return false;

  try {
    return roomURL(sender.url, config.platform) === config.room &&
      roomURL(message.url, config.platform) === config.room;
  } catch {
    return false;
  }
}

export function normalizeEvents(events) {
  if (!Array.isArray(events) || events.length > BATCH_LIMIT) throw new Error('消息批次无效');
  const normalized = [];
  const seen = new Set();
  for (const event of events) {
    if (!event || !['enter', 'gift', 'like', 'comment'].includes(event.kind)) continue;
    if (![event.id, event.nickname, event.text].every(value => typeof value === 'string')) continue;
    const id = event.id.trim();
    const nickname = event.nickname.trim();
    const text = event.text.trim();
    if (!id || id.length > 50 || !nickname || seen.has(id)) continue;
    if (event.kind === 'comment' && !text) continue;
    if (event.kind === 'gift' && !/^.{1,60} x[1-9]\d{0,3}$/u.test(text)) continue;
    seen.add(id);
    normalized.push({
      id, kind: event.kind,
      nickname: Array.from(nickname).slice(0, 100).join(''),
      text: event.kind === 'enter' || event.kind === 'like' ? '' : Array.from(text).slice(0, 500).join(''),
    });
  }
  return normalized;
}
