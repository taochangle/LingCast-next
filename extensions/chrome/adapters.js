(() => {
  function isVisible(element) {
    if (!element || element.closest('[hidden], [aria-hidden="true"]')) return false;
    if (typeof getComputedStyle !== 'function') return true;
    for (let node = element; node; node = node.parentElement) {
      const style = getComputedStyle(node);
      if (style.display === 'none' || style.opacity === '0') return false;
    }
    const { visibility } = getComputedStyle(element);
    return visibility !== 'hidden' && visibility !== 'collapse';
  }

  function visibleText(element, omit) {
    if (!isVisible(element) || element === omit) return '';
    let text = '';
    for (const child of element.childNodes) {
      if (child.nodeType === 3) text += child.textContent;
      if (child.nodeType === 1) text += visibleText(child, omit);
    }
    return text;
  }

  function comment(nickname, text) {
    return text ? { kind: 'comment', nickname, text } : null;
  }

  function gift(nickname, name, count) {
    if (!/^.{1,60}$/u.test(name) || !/^[1-9]\d{0,3}$/.test(count)) return null;
    return { kind: 'gift', nickname, text: `${name} x${count}` };
  }

  const douyin = {
    root: '#chatroom .webcast-chatroom',
    selector: '.webcast-chatroom___item',
    parse(row) {
      const author = row.querySelector('.webcast-chatroom___nickname');
      const label = visibleText(author).trim();
      const nickname = label.replace(/[：:]$/, '').trim();
      if (!nickname) return null;

      const notice = visibleText(row, author).replace(/\s+/g, '');
      if (row.matches('[data-e2e="live-gift-message"]')) {
        const name = visibleText(row.querySelector('[data-e2e="gift-name"]')).trim();
        const count = visibleText(row.querySelector('[data-e2e="gift-count"]')).trim().replace(/^[x×]\s*/u, '');
        return notice.startsWith('送出') ? gift(nickname, name, count) : null;
      }
      if (row.matches('[data-e2e="live-enter-message"]')) {
        return /^(?:进入直播间|来了)$/.test(notice) ? { kind: 'enter', nickname, text: '' } : null;
      }
      if (row.matches('[data-e2e="live-like-message"]')) {
        return /^(?:为主播)?点赞了[！!。]?$/.test(notice) ? { kind: 'like', nickname, text: '' } : null;
      }

      // A chat row has an explicit nickname delimiter and a distinct message body.
      const display = author.closest('.webcast-chatroom___display-text');
      if (!display || !/[：:]$/.test(label)) return null;
      return comment(nickname, visibleText(display, author).trim());
    },
  };

  const xiaohongshu = {
    root: '#live-layout',
    selector: '.msg-wrapper, .enter-msgs .msg-content',
    parse(row) {
      if (row.matches('.enter-msgs .msg-content') && row.closest('.msg-wrapper')) return null;
      if (row.closest('.role-host, .role-system')) return null;
      const author = row.querySelector('.nickname');
      const nickname = visibleText(author).trim();
      if (!nickname) return null;
      const body = visibleText(row, author).trim();
      if (row.closest('.enter-msgs') && body === '来了') return { kind: 'enter', nickname, text: '' };
      if (row.matches('.role-gift')) {
        const match = body.match(/^送出\s*(.{1,60}?)\s*[x×]\s*([1-9]\d{0,3})$/u);
        return match ? gift(nickname, match[1].trim(), match[2]) : null;
      }
      if (row.matches('.role-like') && /^(?:为主播)?点赞了[！!。]?$/.test(body.replace(/\s+/g, ''))) {
        return { kind: 'like', nickname, text: '' };
      }
      if (!row.matches('.msg-wrapper.role-user') || row.matches('.role-notice, .role-like')) return null;
      return comment(nickname, visibleText(row.querySelector('.content')).trim());
    },
  };

  globalThis.LingCastAdapters = { douyin, xiaohongshu };
})();
