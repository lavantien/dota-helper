(function () {
'use strict';

function mount(opts) {
  const G = opts.G;
  const host = document.getElementById('subpools');
  if (!host || host.firstChild) return;

  const el = (tag, cls, text) => {
    const n = document.createElement(tag);
    if (tag === 'button') n.type = 'button';
    if (cls) n.className = cls;
    if (text !== undefined) n.textContent = text;
    return n;
  };

  const ICON_BASE = 'https://cdn.cloudflare.steamstatic.com/apps/dota2/images/dota_react/heroes/';

  const state = { subsets: [], activeId: 0, pairs: new Set(), offline: false, err: '' };

  const key = p => p.slug + '@' + p.role;

  const GROUPS = G.roles.map(r => ({
    role: r,
    chips: (opts.pairs || []).filter(p => p.role === r.id),
  }));
  const MASTER = new Set();
  GROUPS.forEach(g => g.chips.forEach(c => MASTER.add(key(c))));

  const active = () => state.subsets.find(s => s.id === state.activeId) || null;

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
      } catch (e) {}
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

  const modeBanner = el('div', 'banner');
  modeBanner.setAttribute('aria-live', 'polite');
  const errBanner = el('div', 'banner');
  errBanner.setAttribute('aria-live', 'polite');
  const emptyHint = el('div', 'sub',
    'no sub-pools yet, name the first one and create it, then toggle pool pairs into it and save');
  const subsetSel = el('select');
  subsetSel.name = 'subset';
  subsetSel.setAttribute('data-fk', 'sel');
  subsetSel.setAttribute('aria-label', 'active sub-pool');
  const renameField = el('input');
  renameField.autocomplete = 'off';
  renameField.setAttribute('data-fk', 'rename');
  renameField.placeholder = 'rename the active sub-pool';
  renameField.setAttribute('aria-label', 'new name for the active sub-pool');
  const delBtn = el('button', 'segbtn', 'delete');
  delBtn.setAttribute('data-fk', 'del');
  delBtn.setAttribute('aria-label', 'delete the active sub-pool');
  const editRow = el('div', 'subrow');
  editRow.hidden = true;
  editRow.appendChild(subsetSel);
  editRow.appendChild(renameField);
  editRow.appendChild(delBtn);
  const createField = el('input');
  createField.autocomplete = 'off';
  createField.setAttribute('data-fk', 'create');
  createField.placeholder = 'name a new sub-pool';
  createField.setAttribute('aria-label', 'new sub-pool name');
  const createBtn = el('button', 'segbtn', 'create');
  createBtn.setAttribute('data-fk', 'createBtn');
  const createRow = el('div', 'subrow');
  createRow.appendChild(createField);
  createRow.appendChild(createBtn);
  const editCard = el('section', 'card');
  editCard.appendChild(modeBanner);
  editCard.appendChild(errBanner);
  editCard.appendChild(emptyHint);
  editCard.appendChild(editRow);
  editCard.appendChild(createRow);
  const saveBtn = el('button', 'segbtn', 'save pairs');
  saveBtn.setAttribute('data-fk', 'save');
  const pairCount = el('span', 'dim');
  const saveline = el('div', 'saveline');
  saveline.hidden = true;
  saveline.appendChild(saveBtn);
  saveline.appendChild(pairCount);
  const grid = el('div', 'poollist');
  const orphans = el('div', 'poollist');
  const gridCard = el('section', 'card');
  gridCard.appendChild(saveline);
  gridCard.appendChild(grid);
  gridCard.appendChild(orphans);
  host.appendChild(editCard);
  host.appendChild(gridCard);

  function renderModeBanner() {
    modeBanner.textContent = '';
    if (!state.offline) return;
    modeBanner.appendChild(el('div', null,
      'sub-pool editing needs the dev server, run make subsets, the pair grid below is read-only'));
  }

  function renderErr() {
    errBanner.textContent = '';
    if (!state.err) return;
    errBanner.appendChild(el('div', null, state.err));
  }

  function renderSelector() {
    const cur = active();
    const writable = !state.offline;
    editRow.hidden = !cur;
    saveline.hidden = !cur;
    emptyHint.hidden = !writable || !!cur;
    subsetSel.textContent = '';
    subsetSel.disabled = !cur;
    state.subsets.forEach(s => {
      const o = document.createElement('option');
      o.value = String(s.id);
      o.textContent = s.name;
      subsetSel.appendChild(o);
    });
    if (cur) subsetSel.value = String(cur.id);
    renameField.value = cur ? cur.name : '';
    renameField.disabled = !cur;
    delBtn.disabled = !cur;
    saveBtn.disabled = !cur;
    createField.disabled = !writable;
    createBtn.disabled = !writable;
  }

  function renderCount() {
    pairCount.textContent = state.pairs.size + ' of ' + MASTER.size + ' pool pairs';
  }

  function renderGrid() {
    grid.textContent = '';
    const editable = !state.offline && !!active();
    GROUPS.forEach(g => {
      grid.appendChild(el('div', 'legendhead', g.role.label));
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
      grid.appendChild(grp);
    });
  }

  function renderOrphans() {
    orphans.textContent = '';
    const cur = active();
    const stale = cur ? cur.entries.filter(p => !MASTER.has(key(p))) : [];
    if (!stale.length) return;
    orphans.appendChild(el('div', 'legendhead', 'orphaned entries, dropped on save'));
    const grp = el('div', 'legendgrp');
    stale.forEach(p => {
      const row = el('span', 'herochip orphan');
      row.appendChild(el('span', 'tag warn', key(p)));
      row.appendChild(el('span', 'dim', 'not derivable from the current pool'));
      grp.appendChild(row);
    });
    orphans.appendChild(grp);
  }

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
      const back = host.querySelector('[data-fk="' + fk + '"]');
      if (back) back.focus();
    }
  }

  async function create() {
    const name = createField.value.trim();
    if (!name) {
      state.err = 'name the sub-pool first';
      renderErr();
      return;
    }
    try {
      const made = await api('POST', '/api/subsets', { name: name });
      state.err = '';
      state.activeId = made.id;
      createField.value = '';
      opts.onMutated();
      await refresh();
    } catch (e) {
      state.err = e.message;
      renderErr();
    }
  }

  async function save() {
    const cur = active();
    if (!cur) return;
    const entries = [...state.pairs].map(k => {
      const at = k.indexOf('@');
      return { slug: k.slice(0, at), role: k.slice(at + 1) };
    });
    const body = { entries: entries };
    const name = renameField.value.trim();
    if (name && name !== cur.name) body.name = name;
    try {
      await api('PUT', '/api/subsets/' + cur.id, body);
      state.err = '';
      opts.onMutated();
      await refresh();
    } catch (e) {
      state.err = e.message;
      renderErr();
    }
  }

  async function del() {
    const cur = active();
    if (!cur) return;
    try {
      await api('DELETE', '/api/subsets/' + cur.id);
      state.err = '';
      opts.onMutated();
      await refresh();
    } catch (e) {
      state.err = e.message;
      renderErr();
    }
  }

  createBtn.onclick = create;
  delBtn.onclick = del;
  saveBtn.onclick = save;
  subsetSel.onchange = () => {
    state.activeId = Number(subsetSel.value) || 0;
    adopt(active());
    renderAll();
  };
  const onEnter = (node, fn) => node.addEventListener('keydown', e => {
    if (e.key === 'Enter') fn();
  });
  onEnter(createField, create);
  onEnter(renameField, save);
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

window.PickerSubsetsManager = { mount: mount };
})();
