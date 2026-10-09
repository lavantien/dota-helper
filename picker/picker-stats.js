(function (root) {
'use strict';

var cachedScore = null;
function score() {
  if (cachedScore) return cachedScore;
  cachedScore = (typeof globalThis !== 'undefined' && globalThis.PickerScore)
    || (typeof require === 'function' ? require('./picker-score.js') : null);
  if (!cachedScore) throw new Error('PickerStats needs PickerScore loaded first');
  return cachedScore;
}

function metaRows(G, roleId) {
  const rows = [];
  for (let p = 0; p < G.poolIdx.length; p++) {
    const entry = G.entries[p] || { roles: [] };
    if (!(entry.roles || []).includes(roleId)) continue;
    const idx = G.poolIdx[p];
    const stats = (G.heroPos || [])[p] || [];
    let top = null;
    for (const s of stats) {
      if (!top || s[1] > top.share) top = { pos: s[0], share: s[1], wr: s[2] };
    }
    let roleWr = null;
    for (const s of stats) {
      if (String(s[0]) === String(roleId)) { roleWr = s[2]; break; }
    }
    const tr = (G.trend || [])[p] || null;
    const prior = (G.prior || [])[p];
    const pop = (G.pop || [])[idx];
    rows.push({
      pos: p, idx: idx, slug: G.slugs[idx], name: G.names[idx], icon: G.icons[idx],
      topPos: top ? top.pos : null,
      topShare: top ? top.share : null,
      topWr: top ? top.wr : null,
      roleWr: roleWr,
      prior: prior === undefined ? null : prior,
      trendWr: tr ? tr[0] : null,
      trendShare: tr ? tr[1] : null,
      pop: pop === undefined ? null : pop,
    });
  }
  return rows;
}

function pairLists(G, poolPos) {
  const parse = score().parsePacked;
  const self = G.poolIdx[poolPos];
  const allies = parse((G.syn || [])[poolPos] || '')
    .filter(r => r.idx !== self)
    .sort((a, b) => b.pct - a.pct || a.idx - b.idx);
  const enemies = parse((G.mu || [])[poolPos] || '')
    .filter(r => r.idx !== self)
    .sort((a, b) => a.pct - b.pct || a.idx - b.idx);
  return { allies: allies, enemies: enemies };
}

function nullish(v) {
  return v === null || v === undefined;
}

function sortRows(rows, key, dir) {
  const sign = dir === 'asc' ? 1 : -1;
  return rows.map((r, i) => [r, i]).sort((a, b) => {
    const va = a[0][key], vb = b[0][key];
    const nulls = (nullish(va) ? 1 : 0) - (nullish(vb) ? 1 : 0);
    if (nulls) return nulls;
    if (nullish(va)) return a[1] - b[1];
    if (va < vb) return -1 * sign;
    if (va > vb) return 1 * sign;
    return a[1] - b[1];
  }).map(pair => pair[0]);
}

const UI = {
  iconBase: 'https://cdn.cloudflare.steamstatic.com/apps/dota2/images/dota_react/heroes/',
  confTags: { 0: ['high', 'tup'], 1: ['med', 'prov'], 2: ['low', 'warn'] },
  cols: [
    { key: 'name', label: 'hero', dir: 'asc' },
    { key: 'topPos', label: 'top pos', dir: 'desc', num: true },
    { key: 'topShare', label: 'share', dir: 'desc', num: true },
    { key: 'topWr', label: 'wr', dir: 'desc', num: true },
    { key: 'roleWr', label: 'wr at role', dir: 'desc', num: true },
    { key: 'prior', label: 'prior', dir: 'desc', num: true },
    { key: 'trendWr', label: 'wr trend', dir: 'desc', num: true, trend: true },
    { key: 'trendShare', label: 'share trend', dir: 'desc', num: true, trend: true },
    { key: 'pop', label: 'pick rate', dir: 'desc', num: true },
  ],
};

function fmtPct(v) {
  return (v * 100).toFixed(1) + '%';
}

function fmtShare(v) {
  return Math.round(v * 100) + '%';
}

function fmtDelta(v, dp) {
  return (v >= 0 ? '+' : '') + v.toFixed(dp);
}

function boot(G) {
  const $ = id => document.getElementById(id);
  const el = (tag, cls, text) => {
    const n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text !== undefined) n.textContent = text;
    return n;
  };

  function heroBadge(idx, small) {
    const img = document.createElement('img');
    img.className = 'hicon' + (small ? ' small' : '');
    img.src = UI.iconBase + G.icons[idx] + '.png';
    img.alt = '';
    img.loading = 'lazy';
    img.onerror = () => {
      const s = el('span', 'hinit', G.names[idx].slice(0, 2).toUpperCase());
      img.replaceWith(s);
    };
    return img;
  }

  const st = { role: G.roles[0].id, sortKey: null, sortDir: null, pairPos: 0 };

  function renderMeta() {
    const box = $('metaChips');
    box.textContent = '';
    const chip = (text, cls) => box.appendChild(el('span', 'tag' + (cls ? ' ' + cls : ''), text));
    if (G.patch) chip(G.patch);
    if (G.date) chip('data ' + G.date);
    if (G.meta && G.meta.bracket) chip(G.meta.bracket);
    if (G.meta && G.meta.window) chip(G.meta.window);
    chip(G.poolIdx.length + ' pool entries', 'prov');
  }

  function renderSeg() {
    const seg = $('roleSeg');
    seg.textContent = '';
    G.roles.forEach(r => {
      const b = el('button', 'segbtn' + (st.role === r.id ? ' on' : ''), r.label);
      b.onclick = () => {
        st.role = r.id;
        renderSeg();
        renderTable();
        seg.children[G.roles.findIndex(x => x.id === r.id)].focus();
      };
      seg.appendChild(b);
    });
  }

  function cellText(row, col) {
    const v = row[col.key];
    if (nullish(v)) return null;
    if (col.key === 'topPos') return 'pos ' + v;
    if (col.key === 'topShare') return fmtShare(v);
    if (col.key === 'trendShare') return fmtDelta(v, 2) + 'pp';
    if (col.trend) return fmtDelta(v, 1);
    if (col.key === 'pop') return fmtPct(v);
    if (col.num) return fmtPct(v);
    return v;
  }

  function renderTable() {
    const table = $('metaTable');
    table.textContent = '';
    const thead = document.createElement('thead');
    const trh = document.createElement('tr');
    UI.cols.forEach(col => {
      const th = document.createElement('th');
      th.scope = 'col';
      if (col.num) th.className = 'num';
      if (st.sortKey === col.key) th.setAttribute('aria-sort',
        st.sortDir === 'asc' ? 'ascending' : 'descending');
      else th.setAttribute('aria-sort', 'none');
      const b = el('button', st.sortKey === col.key ? 'on' : null, col.label);
      b.onclick = () => {
        if (st.sortKey === col.key) st.sortDir = st.sortDir === 'asc' ? 'desc' : 'asc';
        else { st.sortKey = col.key; st.sortDir = col.dir; }
        renderTable();
      };
      th.appendChild(b);
      trh.appendChild(th);
    });
    thead.appendChild(trh);
    table.appendChild(thead);
    const tbody = document.createElement('tbody');
    let rows = metaRows(G, st.role);
    if (st.sortKey) rows = sortRows(rows, st.sortKey, st.sortDir);
    rows.forEach(row => {
      const tr = document.createElement('tr');
      UI.cols.forEach(col => {
        const td = document.createElement('td');
        if (col.num) td.className = 'num';
        if (col.key === 'name') {
          td.appendChild(heroBadge(row.idx, true));
          td.appendChild(el('span', 'pname', row.name));
        } else {
          const v = cellText(row, col);
          td.textContent = v === null ? '-' : v;
          if (col.trend && !nullish(row[col.key])) {
            td.classList.add(row[col.key] >= 0 ? 'up' : 'down');
          }
        }
        tr.appendChild(td);
      });
      tbody.appendChild(tr);
    });
    table.appendChild(tbody);
  }

  function renderPairPick() {
    const sel = $('pairSel');
    sel.textContent = '';
    G.roles.forEach(r => {
      const group = el('optgroup');
      group.label = r.label;
      let any = false;
      for (let p = 0; p < G.poolIdx.length; p++) {
        const entry = G.entries[p] || { roles: [] };
        if (!(entry.roles || []).includes(r.id)) continue;
        const opt = el('option', null, G.names[G.poolIdx[p]]);
        opt.value = String(p);
        if (p === st.pairPos) opt.selected = true;
        group.appendChild(opt);
        any = true;
      }
      if (any) sel.appendChild(group);
    });
    sel.onchange = () => {
      st.pairPos = Number(sel.value);
      renderPair();
    };
  }

  function pairRow(r) {
    const row = el('div', 'prow');
    row.appendChild(heroBadge(r.idx, true));
    row.appendChild(el('span', 'pname', G.names[r.idx]));
    row.appendChild(el('span', 'ppct', fmtPct(r.pct)));
    const tag = UI.confTags[r.conf] || UI.confTags[2];
    row.appendChild(el('span', 'tag ' + tag[1], tag[0]));
    return row;
  }

  function renderPair() {
    const idx = G.poolIdx[st.pairPos];
    const head = $('pairHead');
    head.textContent = '';
    head.appendChild(heroBadge(idx));
    const entry = G.entries[st.pairPos] || { roles: [] };
    const seat = (entry.roles || []).join('/');
    head.appendChild(el('span', 'rname', G.names[idx] + ' at pos ' + seat));
    const { allies, enemies } = pairLists(G, st.pairPos);
    const fill = (id, rows) => {
      const box = $(id);
      box.textContent = '';
      rows.forEach(r => box.appendChild(pairRow(r)));
    };
    fill('allyList', allies);
    fill('enemyList', enemies);
  }

  renderMeta();
  renderSeg();
  renderTable();
  renderPairPick();
  renderPair();
}

const PickerStats = { metaRows: metaRows, pairLists: pairLists, sortRows: sortRows };
if (typeof module === 'object' && module.exports) module.exports = PickerStats;
else {
  root.PickerStats = PickerStats;
  if (root.PICKER_GEN) boot(root.PICKER_GEN);
}
})(typeof self !== 'undefined' ? self : this);
