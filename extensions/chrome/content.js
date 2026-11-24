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
      if (baseline || (!previous && wasBaseline) || (previous?.signature === signature && !previous.domId)) return;
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
    active.rootObserver.disconnect();
    clearInterval(active.uploadTimer);
    active = null;
  }

  function start(runId, platform) {
    const adapter = Object.hasOwn(LingCastAdapters, platform) ? LingCastAdapters[platform] : null;
    if (!adapter) return false;
    stop();
    const queue = createQueue(adapter);
    const run = { runId, platform, uploading: false };
    let root;
    let pendingBatch;
    let expiredInFlight = 0;

    run.rootObserver = new MutationObserver(records => {
      const rows = new Set();
      function include(row) {
        if (row && rows.size < dedupLimit) rows.add(row);
      }
      function includeDescendants(element) {
        if (rows.size >= dedupLimit) return;
        for (const row of element.querySelectorAll(adapter.selector)) {
          if (rows.size >= dedupLimit) break;
          include(row);
        }
      }
      for (const record of records) {
        const target = record.target.nodeType === 1 ? record.target : record.target.parentElement;
        const owner = target?.closest(adapter.selector);
        if (owner && root.contains(owner)) include(owner);
        if (record.type === 'attributes' && target && root.contains(target)) includeDescendants(target);
        for (const node of record.addedNodes) {
          if (node.nodeType !== 1 || (node.isConnected && !root.contains(node))) continue;
          // A row added inside the room can already be detached by the next DOM commit.
          if (node.matches(adapter.selector)) include(node);
          includeDescendants(node);
        }
      }
      for (const row of rows) queue.inspect(row);
    });

    function updateRoot() {
      const next = document.querySelector(adapter.root);
      if (root === next) return;
      run.rootObserver.disconnect();
      root = next;
      if (!root) return;
      queue.baseline(root);
      run.rootObserver.observe(root, {
        childList: true,
        subtree: true,
        characterData: true,
        attributes: true,
        attributeFilter: ['class', 'data-message-id', 'data-e2e', 'hidden', 'aria-hidden', 'style'],
      });
    }
    updateRoot();
    run.observer = new MutationObserver(updateRoot);
    run.observer.observe(document.documentElement, {
      childList: true, subtree: true, attributes: true, attributeFilter: ['id', 'class'],
    });

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
        if (result?.stop) {
          stop();
          return;
        }
        if (result?.ok && Array.isArray(result.processedIds)) {
          const processed = new Set(result.processedIds);
          pendingBatch.events = pendingBatch.events.filter(event => !processed.has(event.id));
          if (!pendingBatch.events.length) pendingBatch = null;
        }
      } catch {
        // The service worker or message channel can restart. Keep the same IDs until it returns.
        if (active === run && !chrome.runtime.id) stop();
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
    if (message.type === 'LC_PING') respond({ ok: active?.runId === message.runId });
    if (message.type === 'LC_STOP' && active?.runId === message.runId) {
      stop();
      respond({ ok: true });
    }
  });
})();
