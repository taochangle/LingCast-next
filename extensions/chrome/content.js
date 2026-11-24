(() => {
  if (globalThis.LingCastContent) return;

  const queueLimit = 100;
  const batchLimit = 20;
  const dedupLimit = 1000;
  const maxEventAgeMs = 30_000;

  function createQueue(adapter) {
    const baselineRows = new WeakSet();
    const seenRows = new WeakMap();
    const seenIds = new Set();
    const entries = [];
    let dropped = 0;

    function inspect(row, baseline = false) {
      const event = adapter.parse(row);
      if (baseline) baselineRows.add(row);
      if (!event) return;
      const signature = JSON.stringify(event);
      const rawId = row.getAttribute('data-message-id');
      const domId = rawId && rawId.length <= 160 ? rawId : null;
      const previous = seenRows.get(row);
      if (previous?.signature === signature && previous.domId === domId) return;
      seenRows.set(row, { signature, domId });
      if (domId) {
        if (seenIds.has(domId)) return;
        seenIds.add(domId);
        if (seenIds.size > dedupLimit) seenIds.delete(seenIds.values().next().value);
      }
      const wasBaseline = baselineRows.delete(row);
      if (baseline || (!previous && wasBaseline)) return;
      entries.push({ event: { ...event, id: crypto.randomUUID() }, observedAt: Date.now() });
      if (entries.length > queueLimit) {
        entries.shift();
        dropped++;
      }
    }

    return {
      inspect,
      get dropped() { return dropped; },
      baseline(root) {
        for (const row of root.querySelectorAll(adapter.selector)) inspect(row, true);
      },
      drain() {
        const cutoff = Date.now() - maxEventAgeMs;
        while (entries.length && entries[0].observedAt < cutoff) {
          entries.shift();
          dropped++;
        }
        return entries.splice(0, batchLimit).map(({ event }) => event);
      },
    };
  }

  globalThis.LingCastContent = { createQueue };
  if (typeof chrome === 'undefined' || !chrome.runtime?.onMessage) return;

  let active;
  function stop() {
    if (!active) return;
    active.observer.disconnect();
    clearTimeout(active.scanTimer);
    clearInterval(active.uploadTimer);
    active = null;
  }

  function start(runId, platform) {
    const adapter = Object.hasOwn(LingCastAdapters, platform) ? LingCastAdapters[platform] : null;
    if (!adapter) return false;
    stop();
    const queue = createQueue(adapter);
    const pendingRows = new Set();
    const run = { runId, platform, uploading: false };
    let root = document.querySelector(adapter.root);
    let pendingBatch;
    let expiredInFlight = 0;
    if (root) queue.baseline(document);

    function collect(element) {
      if (!element || pendingRows.size >= dedupLimit) return;
      const row = element.closest(adapter.selector);
      if (row) pendingRows.add(row);
      for (const descendant of element.querySelectorAll(adapter.selector)) {
        if (pendingRows.size >= dedupLimit) break;
        pendingRows.add(descendant);
      }
    }

    run.observer = new MutationObserver(records => {
      const nextRoot = document.querySelector(adapter.root);
      if (root !== nextRoot) {
        root = nextRoot;
        pendingRows.clear();
        if (root) queue.baseline(document);
        return;
      }
      if (!root) return;
      for (const record of records) {
        const target = record.target.nodeType === 1 ? record.target : record.target.parentElement;
        const currentRow = target?.closest(adapter.selector);
        if (currentRow && pendingRows.size < dedupLimit) pendingRows.add(currentRow);
        for (const node of record.addedNodes) if (node.nodeType === 1) collect(node);
      }
      if (run.scanTimer || !pendingRows.size) return;
      run.scanTimer = setTimeout(() => {
        run.scanTimer = null;
        for (const row of pendingRows) if (row.isConnected) queue.inspect(row);
        pendingRows.clear();
      }, 150);
    });
    run.observer.observe(document.documentElement, { childList: true, subtree: true, characterData: true });

    run.uploadTimer = setInterval(async () => {
      if (run.uploading) return;
      if (pendingBatch && Date.now() - pendingBatch.createdAt > maxEventAgeMs) {
        expiredInFlight += pendingBatch.events.length;
        pendingBatch = null;
      }
      pendingBatch ??= { events: queue.drain(), createdAt: Date.now() };
      run.uploading = true;
      try {
        // Empty batches serve as a local heartbeat for navigation and stop detection.
        const result = await chrome.runtime.sendMessage({
          type: 'LC_EVENTS', runId, platform, url: location.href, events: pendingBatch.events,
          ready: Boolean(root), discarded: queue.dropped + expiredInFlight,
        });
        if (active !== run) return;
        if (result?.stop) stop();
        if (result?.ok) pendingBatch = null;
      } catch {
        if (active === run) stop();
      } finally {
        run.uploading = false;
      }
    }, 1000);
    active = run;
    return true;
  }

  chrome.runtime.onMessage.addListener((message, sender, respond) => {
    if (sender.id !== chrome.runtime.id || sender.tab) return;
    if (message.type === 'LC_BEGIN') respond({ ok: start(message.runId, message.platform) });
    if (message.type === 'LC_STOP' && active?.runId === message.runId) {
      stop();
      respond({ ok: true });
    }
  });
})();
