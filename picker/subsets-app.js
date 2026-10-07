// subsets-app.js: sub-pool manager page over the loopback /api/subsets
// writer. the master pair grid derives from PICKER_GEN alone, chip toggles
// edit a local pair set, one save PUT sends the whole set plus a rename when
// the field moved, and every mutation re-renders from a fresh GET so the ui
// never drifts from the db. file:// or a fetch failure drops the page to a
// read-only grid with a banner naming make subsets
(function () {
'use strict';

const G = window.PICKER_GEN;

const $ = id => document.getElementById(id);
const el = (tag, cls, text) => {
  const n = document.createElement(tag);
  if (tag === 'button') n.type = 'button';
  if (cls) n.className = cls;
  if (text !== undefined) n.textContent = text;
  return n;
};

// presentation-only constant, mirrors picker-app.js. pool, roles, and every
// pair come from PICKER_GEN
const ICON_BASE = 'https://cdn.cloudflare.steamstatic.com/apps/dota2/images/dota_react/heroes/';

// last GET's subset list, the active subset id, the local slug@role pair
// set, and the offline lock
const state = { subsets: [], activeId: 0, pairs: new Set(), offline: false, err: '' };

const key = p => p.slug + '@' + p.role;

// master (slug, role) pairs straight from the pool-parallel arrays: for
// each pool position one pair per role it plays. grouped by role in
// G.roles order, plain pool order within a role
const GROUPS = G.roles.map(r => {
  const chips = [];
  for (let p = 0; p < G.poolIdx.length; p++) {
    const entry = G.entries[p] || { roles: [] };
    if (entry.roles.includes(r.id)) {
      chips.push({ slug: G.slugs[G.poolIdx[p]], role: r.id, idx: G.poolIdx[p] });
    }
  }
  return { role: r, chips: chips };
});
const MASTER = new Set();
GROUPS.forEach(g => g.chips.forEach(c => MASTER.add(key(c))));

const active = () => state.subsets.find(s => s.id === state.activeId) || null;

// pair set from a subset: stale keys stay out, they render flagged below
// and the next save drops them
function adopt(subset) {
  state.pairs = new Set((subset ? subset.entries : []).filter(p => MASTER.has(key(p))).map(key));
}

function heroBadge(idx) {
  const wrap = el('span');
  const img = document.createElement('img');
  img.className = 'hicon';
  img.src = ICON_BASE + G.icons[idx] + '.png';
  img.alt = G.names[idx];
  img.onerror = () => {
    const s = el('span', 'hinit', G.names[idx].slice(0, 2).toUpperCase());
    img.replaceWith(s);
  };
  wrap.appendChild(img);
  return wrap;
}

// one wrapper for the whole api: non-2xx replies surface their error text,
// a rejected fetch names the dev server
async function api(method, path, body) {
  let r;
  try {
    const opt = { method: method };
    if (body !== undefined) {
      opt.headers = { 'Content-Type': 'application/json' };
      opt.body = JSON.stringify(body);
    }
    r = await fetch(path, opt);
  } catch (e) {
    throw new Error('dev server unreachable, run make subsets');
  }
  if (!r.ok) {
    let msg = 'http ' + r.status;
    try {
      const j = await r.json();
      if (j && j.error) msg = j.error;
    } catch (e) { /* body was not json, keep the status line */ }
    throw new Error(msg);
  }
  return r.status === 204 ? null : r.json();
}

async function refresh() {
  const raw = await api('GET', '/api/subsets');
  state.subsets = (raw && raw.subsets) || [];
  if (!state.subsets.some(s => s.id === state.activeId)) {
    state.activeId = state.subsets.length ? state.subsets[0].id : 0;
  }
  adopt(active());
  renderAll();
}

function renderModeBanner() {
  const box = $('modeBanner');
  box.textContent = '';
  if (!state.offline) return;
  box.appendChild(el('div', null,
    'sub-pool editing needs the dev server, run make subsets, the pair grid below is read-only'));
}

function renderErr() {
  const box = $('errBanner');
  box.textContent = '';
  if (!state.err) return;
  box.appendChild(el('div', null, state.err));
}

function renderSelector() {
  const cur = active();
  const writable = !state.offline;
  $('editRow').hidden = !cur;
  $('saveline').hidden = !cur;
  $('emptyHint').hidden = !writable || !!cur;
  const sel = $('subsetSel');
  sel.textContent = '';
  sel.disabled = !cur;
  state.subsets.forEach(s => {
    const o = document.createElement('option');
    o.value = String(s.id);
    o.textContent = s.name;
    sel.appendChild(o);
  });
  if (cur) sel.value = String(cur.id);
  $('renameField').value = cur ? cur.name : '';
  $('renameField').disabled = !cur;
  $('delBtn').disabled = !cur;
  $('saveBtn').disabled = !cur;
  $('createField').disabled = !writable;
  $('createBtn').disabled = !writable;
}

function renderCount() {
  $('pairCount').textContent = state.pairs.size + ' of ' + MASTER.size + ' pool pairs';
}

function renderGrid() {
  const box = $('grid');
  box.textContent = '';
  const editable = !state.offline && !!active();
  GROUPS.forEach(g => {
    box.appendChild(el('div', 'legendhead', g.role.label));
    const grp = el('div', 'legendgrp');
    g.chips.forEach(c => {
      const k = key(c);
      const chip = el('button', 'herochip');
      chip.appendChild(heroBadge(c.idx));
      chip.appendChild(el('span', null, G.names[c.idx]));
      chip.dataset.fk = k;
      chip.disabled = !editable;
      const paint = on => {
        chip.classList.toggle('on', on);
        chip.setAttribute('aria-pressed', on ? 'true' : 'false');
      };
      paint(state.pairs.has(k));
      chip.setAttribute('aria-label', G.names[c.idx] + ', ' + g.role.label + ', enter to toggle');
      chip.onclick = () => {
        if (state.pairs.has(k)) state.pairs.delete(k);
        else state.pairs.add(k);
        paint(state.pairs.has(k));
        renderCount();
      };
      grp.appendChild(chip);
    });
    box.appendChild(grp);
  });
}

// stale subset entries, slug or role not derivable from the master pairs,
// render flagged and never join the chip grid
function renderOrphans() {
  const box = $('orphans');
  box.textContent = '';
  const cur = active();
  const stale = cur ? cur.entries.filter(p => !MASTER.has(key(p))) : [];
  if (!stale.length) return;
  box.appendChild(el('div', 'legendhead', 'orphaned entries, dropped on save'));
  const grp = el('div', 'legendgrp');
  stale.forEach(p => {
    const row = el('span', 'herochip orphan');
    row.appendChild(el('span', 'tag warn', key(p)));
    row.appendChild(el('span', 'dim', 'not derivable from the current pool'));
    grp.appendChild(row);
  });
  box.appendChild(grp);
}

// full re-render, focus returns to the control that had it where the
// picker-app idiom applies (every control carries data-fk)
function renderAll() {
  const ae = document.activeElement;
  const fk = ae && ae.dataset ? (ae.dataset.fk || '') : '';
  renderModeBanner();
  renderErr();
  renderSelector();
  renderGrid();
  renderOrphans();
  renderCount();
  if (fk) {
    const back = document.querySelector('[data-fk="' + fk + '"]');
    if (back) back.focus();
  }
}

async function create() {
  const name = $('createField').value.trim();
  if (!name) {
    state.err = 'name the sub-pool first';
    renderErr();
    return;
  }
  try {
    const made = await api('POST', '/api/subsets', { name: name });
    state.err = '';
    state.activeId = made.id;
    $('createField').value = '';
    await refresh();
  } catch (e) {
    state.err = e.message;
    renderErr();
  }
}

// one PUT carries the full pair set, plus the new name when the rename
// field moved off the current name
async function save() {
  const cur = active();
  if (!cur) return;
  const entries = [...state.pairs].map(k => {
    const at = k.indexOf('@');
    return { slug: k.slice(0, at), role: k.slice(at + 1) };
  });
  const body = { entries: entries };
  const name = $('renameField').value.trim();
  if (name && name !== cur.name) body.name = name;
  try {
    await api('PUT', '/api/subsets/' + cur.id, body);
    state.err = '';
    await refresh();
  } catch (e) {
    state.err = e.message;
    renderErr();
  }
}

// immediate, no confirm dialog, the delete button sits a card away from save
async function del() {
  const cur = active();
  if (!cur) return;
  try {
    await api('DELETE', '/api/subsets/' + cur.id);
    state.err = '';
    await refresh();
  } catch (e) {
    state.err = e.message;
    renderErr();
  }
}

function boot() {
  $('createBtn').onclick = create;
  $('delBtn').onclick = del;
  $('saveBtn').onclick = save;
  $('subsetSel').onchange = () => {
    state.activeId = Number($('subsetSel').value) || 0;
    adopt(active());
    renderAll();
  };
  const onEnter = (id, fn) => $(id).addEventListener('keydown', e => {
    if (e.key === 'Enter') fn();
  });
  onEnter('createField', create);
  onEnter('renameField', save);
  if (location.protocol === 'file:') {
    state.offline = true;
    renderAll();
    return;
  }
  refresh().catch(() => {
    state.offline = true;
    renderAll();
  });
}
boot();
})();
