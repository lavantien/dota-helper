// picker-subset.js: active-subset view of the master pool
// computes a slug@role membership view, swaps the pool-parallel arrays in
// place behind a restore closure, and renders the subset bar over the dev
// server's /api/subsets. dual env like picker-score.js: module.exports for
// node tests, plain global for the page, boot() is browser only
(function (root) {
'use strict';

// pool-parallel arrays swapped by apply, the snapshot list for the round trip
const SWAPS = ['poolIdx', 'entries', 'prose', 'mu', 'syn', 'prior', 'heroPos', 'trend'];

// membership view of the pool against slug@role pairs. unknown pairs grant
// nothing anywhere, so a stale read-side list reconciles silently
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

// swap the sliced arrays in, clone gates with the filtered fallbackOrder so
// the pristine object graph stays untouched, and drop the picker-score memo
// cache: parsed mu/syn and slugIdx key on the pool, a stale cache after the
// swap scores the wrong rows. restore swaps the snapshot back, cache dropped
// again
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

// browser only: the subset bar. /api/subsets answers {subsets: [{id, name,
// entries: [{slug, role}]}]} same-origin, so file:// never fetches.
// file://, fetch failure, or zero subsets leave the full pool untouched and
// the bar empty, a ?subset= name that cannot load gets a hint chip instead
function boot(opts) {
  const G = opts.G;
  const bar = document.getElementById('subsetBar');
  if (!bar) return;
  const wanted = new URLSearchParams(location.search).get('subset') || '';
  const chip = text => {
    const c = document.createElement('span');
    c.className = 'tag warn';
    c.textContent = text;
    bar.appendChild(c);
  };
  const offline = () => { if (wanted) chip('subset list needs the dev server'); };
  function mountBar(subsets) {
    const byName = {};
    subsets.forEach(s => { byName[s.name] = s; });
    bar.textContent = '';
    let restore = null;
    let current = '';
    const sel = document.createElement('select');
    sel.setAttribute('aria-label', 'pool subset');
    const opt = (value, text) => {
      const o = document.createElement('option');
      o.value = value;
      o.textContent = text;
      sel.appendChild(o);
    };
    opt('', 'full pool');
    subsets.forEach(s => opt(s.name, s.name));
    const clear = document.createElement('button');
    clear.type = 'button';
    clear.className = 'segbtn';
    clear.textContent = 'clear';
    clear.disabled = true;
    const set = name => {
      if (name === current) return;
      if (restore) { restore(); restore = null; }
      current = name;
      if (name) restore = apply(G, byName[name].entries);
      clear.disabled = !name;
      opts.onChange();
    };
    sel.onchange = () => set(sel.value);
    clear.onclick = () => { sel.selectedIndex = 0; set(''); };
    bar.appendChild(sel);
    bar.appendChild(clear);
    if (!wanted) return;
    if (byName[wanted]) set(wanted);
    else chip('subset not found: ' + wanted);
  }
  if (location.protocol === 'file:') { offline(); return; }
  fetch('/api/subsets')
    .then(r => { if (!r.ok) throw new Error('http ' + r.status); return r.json(); })
    .then(raw => {
      const subsets = Array.isArray(raw) ? raw : (raw && raw.subsets) || [];
      if (subsets.length) mountBar(subsets);
      else offline();
    })
    .catch(offline);
}

const PickerSubset = { computeSubset: computeSubset, apply: apply, boot: boot };
if (typeof module === 'object' && module.exports) module.exports = PickerSubset;
else root.PickerSubset = PickerSubset;
})(typeof self !== 'undefined' ? self : this);
