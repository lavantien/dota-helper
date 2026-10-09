(function (root) {
'use strict';

const SWAPS = ['poolIdx', 'entries', 'prose', 'mu', 'syn', 'prior', 'heroPos', 'trend'];

function computeSubset(G, pairs) {
  const set = new Set();
  (pairs || []).forEach(p => { set.add(p.slug + '@' + p.role); });
  const kept = [];
  const entries = [];
  for (let p = 0; p < G.poolIdx.length; p++) {
    const e = G.entries[p] || { roles: [], tier: {} };
    const roles = e.roles.filter(r => set.has(G.slugs[G.poolIdx[p]] + '@' + r));
    if (!roles.length) continue;
    const tier = {};
    roles.forEach(r => { if (e.tier && e.tier[r] !== undefined) tier[r] = e.tier[r]; });
    kept.push(p);
    entries.push({ roles: roles, tier: tier });
  }
  const fb = (G.gates && G.gates.fallbackOrder) || {};
  const fallbackOrder = {};
  Object.keys(fb).forEach(role => {
    fallbackOrder[role] = fb[role].filter(slug => set.has(slug + '@' + role));
  });
  return { kept: kept, entries: entries, fallbackOrder: fallbackOrder };
}

function apply(G, pairs) {
  const sub = computeSubset(G, pairs);
  const orig = { gates: G.gates };
  SWAPS.forEach(k => { orig[k] = G[k]; });
  SWAPS.forEach(k => { G[k] = sub.kept.map(p => (orig[k] || [])[p]); });
  G.entries = sub.entries;
  G.gates = Object.assign({}, G.gates, { fallbackOrder: sub.fallbackOrder });
  delete G.__psCache;
  return function restore() {
    SWAPS.forEach(k => { G[k] = orig[k]; });
    G.gates = orig.gates;
    delete G.__psCache;
  };
}

function boot(opts) {
  const G = opts.G;
  const bar = document.getElementById('subsetBar');
  const inert = { reload: () => Promise.resolve() };
  if (!bar) return inert;
  const wanted = new URLSearchParams(location.search).get('subset') || '';
  const chip = text => {
    const c = document.createElement('span');
    c.className = 'tag warn';
    c.textContent = text;
    bar.appendChild(c);
  };
  const offline = () => { if (wanted) chip('subset list needs the dev server'); };
  let view = null;
  function mountBar(subsets, keepId) {
    const byId = {};
    subsets.forEach(s => { byId[s.id] = s; });
    bar.textContent = '';
    const sel = document.createElement('select');
    sel.setAttribute('aria-label', 'pool subset');
    const opt = (value, text) => {
      const o = document.createElement('option');
      o.value = value;
      o.textContent = text;
      sel.appendChild(o);
    };
    opt('0', 'full pool');
    subsets.forEach(s => opt(String(s.id), s.name));
    const clear = document.createElement('button');
    clear.type = 'button';
    clear.className = 'segbtn';
    clear.textContent = 'clear';
    clear.disabled = true;
    const v = { byId: byId, currentId: 0, restore: null, sel: sel, clear: clear, setById: null };
    const setById = id => {
      if (id === v.currentId) return;
      if (v.restore) { v.restore(); v.restore = null; }
      v.currentId = id;
      if (byId[id]) v.restore = apply(G, byId[id].entries);
      sel.value = String(id);
      clear.disabled = !id;
      opts.onChange();
    };
    v.setById = setById;
    sel.onchange = () => setById(Number(sel.value) || 0);
    clear.onclick = () => { sel.selectedIndex = 0; setById(0); };
    bar.appendChild(sel);
    bar.appendChild(clear);
    view = v;
    if (keepId && byId[keepId]) setById(keepId);
  }
  const fetchList = () => fetch('/api/subsets')
    .then(r => { if (!r.ok) throw new Error('http ' + r.status); return r.json(); })
    .then(raw => (Array.isArray(raw) ? raw : (raw && raw.subsets) || []));
  function reload() {
    if (location.protocol === 'file:') return Promise.resolve();
    return fetchList().then(subsets => {
      const prev = view;
      const prevId = prev ? prev.currentId : 0;
      if (prev && prev.restore) prev.restore();
      if (!subsets.length) {
        if (!prev) { offline(); return; }
        mountBar([], 0);
        if (prevId) opts.onChange();
        return;
      }
      mountBar(subsets, prevId);
      if (prevId && !view.byId[prevId]) opts.onChange();
    }).catch(() => {});
  }
  if (location.protocol === 'file:') { offline(); return inert; }
  fetchList()
    .then(subsets => {
      if (!subsets.length) { offline(); return; }
      mountBar(subsets, 0);
      if (!wanted) return;
      const hit = subsets.find(s => s.name === wanted);
      if (hit) view.setById(hit.id);
      else chip('subset not found: ' + wanted);
    })
    .catch(offline);
  return { reload: reload };
}

const PickerSubset = { computeSubset: computeSubset, apply: apply, boot: boot };
if (typeof module === 'object' && module.exports) module.exports = PickerSubset;
else root.PickerSubset = PickerSubset;
})(typeof self !== 'undefined' ? self : this);
