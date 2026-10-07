// picker-search.js: fuzzy roster search and the autocomplete dropdown.
// mounted and driven by picker-app.js through opts closures, reads a
// PICKER_GEN-shaped object only, no fetch, file:// safe
(function () {
'use strict';

function mount(opts) {
  const G = opts.G;
  const input = opts.input;
  const list = opts.list;

  // match keys precomputed once per mount: lowercase display text, word
  // initials ("Phantom Lancer" -> "pl"), and the emitted legacy aliases
  const lowers = G.names.map(n => n.toLowerCase());
  const slugLowers = G.slugs.map(s => s.toLowerCase());
  const initialsOf = s => s.split(/[\s'\-]+/).filter(Boolean).map(w => w[0]).join('');
  const initials = G.names.map(initialsOf);
  const slugInitials = G.slugs.map(s => s.split('-').filter(Boolean).map(p => p[0]).join(''));
  const aliases = [];
  Object.keys(G.aliases || {}).forEach(key => {
    const idx = G.slugs.indexOf(G.aliases[key]);
    if (idx >= 0) aliases.push({ idx: idx, keyLower: key.toLowerCase(), init: initialsOf(key) });
  });
  // repertoire order comes from the app (role-grouped, authored within role)
  // so the empty-query browse matches the legend; plain poolIdx is the fallback.
  // mutable: a subset swap re-points both through setPoolOrder
  let poolOrder = opts.poolOrder || G.poolIdx;
  let poolSet = new Set(poolOrder);

  let sel = 0;
  let visible = false;
  let rows = []; // selectable (untaken) result roster indices

  // greedy subsequence quality, tighter left-aligned matches score higher,
  // null when q does not consume fully
  function subseq(q, s) {
    let at = 0, matched = 0, gaps = 0, prev = -1;
    for (let i = 0; i < s.length && at < q.length; i++) {
      if (s[i] === q[at]) {
        matched++;
        if (prev >= 0) gaps += i - prev - 1;
        prev = i;
        at++;
      }
    }
    return at === q.length ? matched - 2 * gaps - 0.5 * (s.length - q.length) : null;
  }

  // matched display-name offsets: substring run (a hyphen in the query reads
  // as the name's space, so slug pastes mark fully), else greedy consumption
  // only when it consumes the whole query
  function marksFor(q, name) {
    const marks = new Set();
    let p = name.indexOf(q);
    if (p < 0 && q.indexOf('-') >= 0) p = name.indexOf(q.replace(/-/g, ' '));
    if (p >= 0) {
      for (let i = 0; i < q.length; i++) marks.add(p + i);
      return marks;
    }
    let at = 0;
    for (let i = 0; i < name.length && at < q.length; i++) {
      if (name[i] === q[at]) { marks.add(i); at++; }
    }
    return at === q.length ? marks : new Set();
  }

  // rank every roster hero against the query. tiers: 0 exact, 1 prefix,
  // 2 initials prefix, 3 substring, 4 subsequence, 5 the empty-query browse
  // order (pool first, then roster order). alias hits adopt the alias's own
  // tier and promote the canonical hero.
  function rank(qRaw) {
    const q = qRaw.trim().toLowerCase();
    if (!q) {
      const out = poolOrder.map(idx => ({ idx: idx, tier: 5 }));
      for (let i = 0; i < G.rosterCount; i++) {
        if (!poolSet.has(i)) out.push({ idx: i, tier: 5 });
      }
      return out;
    }
    const out = [];
    for (let i = 0; i < G.rosterCount; i++) {
      const name = lowers[i];
      let tier = -1;
      let fromName = false;
      if (name === q || slugLowers[i] === q) { tier = 0; fromName = true; }
      else if (name.startsWith(q) || slugLowers[i].startsWith(q)) { tier = 1; fromName = true; }
      else if (initials[i].startsWith(q) || slugInitials[i].startsWith(q)) { tier = 2; fromName = true; }
      else if (name.includes(q) || slugLowers[i].includes(q)) { tier = 3; fromName = true; }
      for (let a = 0; a < aliases.length; a++) {
        const al = aliases[a];
        if (al.idx !== i) continue;
        let at = -1;
        if (al.keyLower === q) at = 0;
        else if (al.keyLower.startsWith(q)) at = 1;
        else if (al.init.startsWith(q)) at = 2;
        else if (al.keyLower.includes(q)) at = 3;
        if (at >= 0 && (tier < 0 || at < tier)) { tier = at; fromName = false; }
      }
      let score = 0;
      if (tier < 0) {
        score = subseq(q, name);
        if (score === null) score = subseq(q, slugLowers[i]);
        if (score !== null) { tier = 4; fromName = true; }
      }
      if (tier >= 0) out.push({ idx: i, tier: tier, score: score, marks: fromName ? marksFor(q, name) : null });
    }
    out.sort((a, b) => a.tier - b.tier
      || b.score - a.score
      || (poolSet.has(b.idx) ? 1 : 0) - (poolSet.has(a.idx) ? 1 : 0)
      || lowers[a.idx].length - lowers[b.idx].length
      || a.idx - b.idx);
    return out;
  }

  function nameSpan(idx, marks) {
    const s = G.names[idx];
    const span = document.createElement('span');
    if (!marks || !marks.size) { span.textContent = s; return span; }
    let buf = '';
    for (let i = 0; i < s.length; i++) {
      if (marks.has(i)) {
        if (buf) { span.appendChild(document.createTextNode(buf)); buf = ''; }
        const m = document.createElement('span');
        m.className = 'hlmark';
        m.textContent = s[i];
        span.appendChild(m);
      } else buf += s[i];
    }
    if (buf) span.appendChild(document.createTextNode(buf));
    return span;
  }

  function hide() {
    visible = false;
    list.hidden = true;
    input.setAttribute('aria-expanded', 'false');
    input.removeAttribute('aria-activedescendant');
  }

  function renderList() {
    list.textContent = '';
    const ranked = rank(input.value);
    if (!ranked.length) { rows = []; hide(); return; }
    // the limit keeps typed result lists tight; the empty query is the browse
    // surface, so it renders the whole roster
    const shown = input.value.trim() ? ranked.slice(0, opts.limit) : ranked;
    const taken = opts.isTaken();
    rows = shown.filter(r => !taken.has(r.idx)).map(r => r.idx);
    if (sel >= rows.length) sel = Math.max(0, rows.length - 1);
    let hlId = null;
    let k = 0; // selectable-row cursor, rows[k] is the row being rendered
    shown.forEach(r => {
      const row = document.createElement('div');
      row.className = 'acrow';
      row.setAttribute('role', 'option');
      row.id = 'aco-' + r.idx;
      row.appendChild(opts.badge(r.idx));
      row.appendChild(nameSpan(r.idx, r.marks));
      if (poolSet.has(r.idx)) {
        const dot = document.createElement('span');
        dot.className = 'poolmark';
        dot.setAttribute('aria-hidden', 'true');
        dot.title = 'repertoire';
        row.appendChild(dot);
      }
      if (taken.has(r.idx)) {
        row.classList.add('out');
        row.setAttribute('aria-disabled', 'true');
      } else {
        const hl = k === sel;
        k++;
        if (hl) { row.classList.add('hl'); hlId = row.id; }
        row.setAttribute('aria-selected', hl ? 'true' : 'false');
        row.onmousedown = e => { e.preventDefault(); opts.onPick(r.idx); };
      }
      list.appendChild(row);
    });
    list.hidden = false;
    visible = true;
    input.setAttribute('aria-expanded', 'true');
    if (hlId) input.setAttribute('aria-activedescendant', hlId);
    else input.removeAttribute('aria-activedescendant');
  }

  input.addEventListener('input', () => { sel = 0; renderList(); });
  input.addEventListener('keydown', e => {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      if (!visible) renderList();
      else if (rows.length) { sel = (sel + 1) % rows.length; renderList(); }
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      if (visible && rows.length) { sel = (sel - 1 + rows.length) % rows.length; renderList(); }
    } else if (e.key === 'Enter') {
      e.preventDefault();
      if (visible && rows.length) opts.onPick(rows[sel]);
    } else if (e.key === 'Escape') {
      hide();
    }
  });
  input.addEventListener('blur', () => setTimeout(hide, 120));

  return {
    refresh: () => { if (visible) renderList(); },
    clearAndHide: () => { input.value = ''; hide(); },
    hide: hide,
    setPoolOrder: order => {
      poolOrder = order || G.poolIdx;
      poolSet = new Set(poolOrder);
    },
  };
}

window.PickerSearch = { mount: mount };
})();
