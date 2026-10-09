(function (root) {
'use strict';

const TABS = ['draft', 'subpools', 'guide', 'stats'];
const FRAMES = {
  guide: { id: 'guideFrame', src: '../guide/index.html' },
  stats: { id: 'statsFrame', src: 'stats.html' },
};

function resolveTab(hash) {
  const tab = (hash || '').replace(/^#/, '');
  return TABS.includes(tab) ? tab : 'draft';
}

function boot(opts) {
  const G = opts.G;
  const subset = window.PickerSubset
    ? window.PickerSubset.boot({ G: G, onChange: opts.onPoolChange }) : null;
  const buttons = {};
  const panes = {};
  TABS.forEach(t => {
    buttons[t] = document.querySelector('#tabbar [data-tab="' + t + '"]');
    panes[t] = document.getElementById('pane-' + t);
  });
  let managerMounted = false;
  function activate(tab) {
    TABS.forEach(t => {
      panes[t].hidden = t !== tab;
      buttons[t].classList.toggle('on', t === tab);
      if (t === tab) buttons[t].setAttribute('aria-current', 'page');
      else buttons[t].removeAttribute('aria-current');
    });
    const frame = FRAMES[tab];
    if (frame) {
      const node = document.getElementById(frame.id);
      if (!node.src) node.src = frame.src;
    }
    if (tab === 'subpools' && !managerMounted && window.PickerSubsetsManager) {
      managerMounted = true;
      window.PickerSubsetsManager.mount({
        G: G,
        onMutated: () => { if (subset) subset.reload(); },
      });
    }
  }
  TABS.forEach(t => {
    buttons[t].onclick = () => { location.hash = '#' + t; };
  });
  const route = () => activate(resolveTab(location.hash));
  window.addEventListener('hashchange', route);
  route();
}

const PickerTabs = { boot: boot, resolveTab: resolveTab };
if (typeof module === 'object' && module.exports) module.exports = PickerTabs;
else root.PickerTabs = PickerTabs;
})(typeof self !== 'undefined' ? self : this);
