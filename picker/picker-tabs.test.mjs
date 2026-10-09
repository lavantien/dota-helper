import { test } from 'node:test';
import assert from 'node:assert/strict';
import PickerTabs from './picker-tabs.js';
import PickerSubset from './picker-subset.js';

const resolveTab = PickerTabs.resolveTab;
const pristinePairs = PickerTabs.pristinePairs;

test('empty and bare hashes resolve to the draft tab', () => {
  assert.equal(resolveTab(''), 'draft');
  assert.equal(resolveTab('#'), 'draft');
  assert.equal(resolveTab(null), 'draft');
  assert.equal(resolveTab(undefined), 'draft');
});

test('each known tab resolves from its own hash', () => {
  assert.equal(resolveTab('#draft'), 'draft');
  assert.equal(resolveTab('#subpools'), 'subpools');
  assert.equal(resolveTab('#guide'), 'guide');
  assert.equal(resolveTab('#stats'), 'stats');
});

test('unknown hashes fall back to draft', () => {
  assert.equal(resolveTab('#bogus'), 'draft');
  assert.equal(resolveTab('#drafts'), 'draft');
  assert.equal(resolveTab('#subpools/extra'), 'draft');
  assert.equal(resolveTab('#DRAFT'), 'draft');
  assert.equal(resolveTab('draft'), 'draft');
});

test('resolveTab never mutates its input', () => {
  const h = '#guide';
  resolveTab(h);
  assert.equal(h, '#guide');
});

const fullG = () => ({
  roles: [{ id: '1', label: 'carry' }, { id: '4', label: 'soft support' }],
  slugs: ['slark', 'necrophos'],
  names: ['Slark', 'Necrophos'],
  icons: ['slark', 'necrophos'],
  poolIdx: [0, 1, 1],
  entries: [
    { roles: ['1', '4'], tier: {} },
    { roles: ['4'], tier: {} },
    { roles: ['1'], tier: {} },
  ],
});

test('pristinePairs flattens every pool seat including multi-role entries', () => {
  const pairs = pristinePairs(fullG());
  assert.deepEqual(pairs.map(p => p.slug + '@' + p.role),
    ['slark@1', 'slark@4', 'necrophos@4', 'necrophos@1']);
  assert.deepEqual(pairs.map(p => p.idx), [0, 0, 1, 1]);
});

test('pristinePairs snapshotted before a subset swap keeps the full universe', () => {
  const G = fullG();
  const before = pristinePairs(G);
  const restore = PickerSubset.apply(G, [{ slug: 'slark', role: '1' }]);
  assert.equal(pristinePairs(G).length, 1);
  restore();
  assert.equal(before.length, 4);
  assert.equal(pristinePairs(G).length, 4);
});
