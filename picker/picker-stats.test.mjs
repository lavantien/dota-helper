import { test } from 'node:test';
import assert from 'node:assert/strict';
import PickerStats from './picker-stats.js';

// fixture: 8 roster heroes, 4 pool entries, hand-authored packed strings
// roster 0 alpha, 1 bravo, 2 charlie, 3 delta, 4 echo, 5 foxtrot, 6 golf, 7 hotel
const ROSTER = ['alpha', 'bravo', 'charlie', 'delta', 'echo', 'foxtrot', 'golf', 'hotel'];
const cap = s => s[0].toUpperCase() + s.slice(1);

function baseGen() {
  return {
    date: '2026-10-07', patch: 't', rosterCount: 8,
    slugs: ROSTER.slice(), names: ROSTER.map(cap), icons: ROSTER.slice(),
    pop: [0.05, 0.2, 0.1, 0.15, 0.02, 0.08, 0.12, 0.01],
    poolIdx: [0, 4, 7, 2],
    entries: [
      { roles: ['1'], tier: { '1': 'dedicated' } },
      { roles: ['3'], tier: { '3': 'flex' } },
      { roles: ['3'], tier: { '3': 'dedicated' } },
      { roles: ['2'], tier: { '2': 'dedicated' } },
    ],
    mu: ['0:0.77:0,1:0.9:0,2:0.2:1,3:0.1:2', '', '5:0.5:2', '1:0.3:1,6:0.85:0'],
    syn: ['2:0.8:0,5:0.6:1,0:0.9:0', '', '4:0.7:2', '7:0.55:2'],
    prior: [0.7, 0.4, 0.1, 0.55],
    heroPos: [
      [[1, 0.9, 0.55], [2, 0.1, 0.45]],
      [],
      [[3, 0.8, 0.52], [1, 0.2, 0.48]],
      [[1, 0.6, 0.5], [5, 0.4, 0.44]],
    ],
    trend: [[1.5, -0.25], null, [0, 0], [-0.5, 0.75]],
    roles: [{ id: '1', label: 'pos 1' }, { id: '2', label: 'pos 2' }, { id: '3', label: 'pos 3' }],
  };
}

test('metaRows filters the pool by role and reads the top heroPos triple', () => {
  const rows = PickerStats.metaRows(baseGen(), '3');
  assert.deepEqual(rows.map(r => r.slug), ['echo', 'hotel']);
  assert.deepEqual(rows[1], {
    pos: 2, idx: 7, slug: 'hotel', name: 'Hotel', icon: 'hotel',
    topPos: 3, topShare: 0.8, topWr: 0.52, roleWr: 0.52,
    prior: 0.1, trendWr: 0, trendShare: 0, pop: 0.01,
  });
});

test('metaRows reads nulls through empty heroPos and null trend', () => {
  const rows = PickerStats.metaRows(baseGen(), '3');
  assert.deepEqual(rows[0], {
    pos: 1, idx: 4, slug: 'echo', name: 'Echo', icon: 'echo',
    topPos: null, topShare: null, topWr: null, roleWr: null,
    prior: 0.4, trendWr: null, trendShare: null, pop: 0.02,
  });
});

test('roleWr is null when heroPos has no row for the viewed role', () => {
  const rows = PickerStats.metaRows(baseGen(), '2');
  assert.equal(rows.length, 1);
  assert.equal(rows[0].slug, 'charlie');
  assert.equal(rows[0].topPos, 1);
  assert.equal(rows[0].topShare, 0.6);
  assert.equal(rows[0].topWr, 0.5);
  assert.equal(rows[0].roleWr, null);
  assert.equal(rows[0].trendWr, -0.5);
  assert.equal(rows[0].trendShare, 0.75);
});

test('metaRows on a role with no pool entries is empty', () => {
  const g = baseGen();
  g.roles.push({ id: '4', label: 'pos 4' });
  assert.deepEqual(PickerStats.metaRows(g, '4'), []);
});

test('metaRows reads a subset-swapped short G without reaching old pool slots', () => {
  const g = baseGen();
  g.poolIdx = [4];
  g.entries = [{ roles: ['3'], tier: { '3': 'flex' } }];
  g.mu = [''];
  g.syn = [''];
  g.prior = [0.4];
  g.heroPos = [[]];
  g.trend = [null];
  const rows = PickerStats.metaRows(g, '3');
  assert.deepEqual(rows.map(r => r.slug), ['echo']);
  assert.equal(rows[0].topPos, null);
  assert.equal(rows[0].trendWr, null);
});

test('pairLists ranks allies desc and enemies asc, self dropped, conf carried', () => {
  const g = baseGen();
  const { allies, enemies } = PickerStats.pairLists(g, 0);
  assert.deepEqual(allies, [
    { idx: 2, pct: 0.8, conf: 0 },
    { idx: 5, pct: 0.6, conf: 1 },
  ]);
  assert.deepEqual(enemies, [
    { idx: 3, pct: 0.1, conf: 2 },
    { idx: 2, pct: 0.2, conf: 1 },
    { idx: 1, pct: 0.9, conf: 0 },
  ]);
});

test('pairLists on empty packed strings returns empty lists', () => {
  assert.deepEqual(PickerStats.pairLists(baseGen(), 1), { allies: [], enemies: [] });
});

test('pairLists breaks pct ties by roster idx ascending', () => {
  const g = baseGen();
  g.syn[0] = '3:0.5:0,2:0.5:1';
  g.mu[0] = '5:0.4:2,4:0.4:0';
  const { allies, enemies } = PickerStats.pairLists(g, 0);
  assert.deepEqual(allies.map(r => r.idx), [2, 3]);
  assert.deepEqual(enemies.map(r => r.idx), [4, 5]);
});

test('sortRows orders by key and direction', () => {
  const rows = PickerStats.metaRows(baseGen(), '3').concat(PickerStats.metaRows(baseGen(), '2'));
  const byWr = PickerStats.sortRows(rows, 'topWr', 'desc');
  assert.deepEqual(byWr.map(r => r.topWr), [0.52, 0.5, null]);
  const byName = PickerStats.sortRows(rows, 'name', 'asc');
  assert.deepEqual(byName.map(r => r.name), ['Charlie', 'Echo', 'Hotel']);
});

test('sortRows is stable on equal keys', () => {
  const rows = [
    { name: 'a', v: 1 },
    { name: 'b', v: 1 },
    { name: 'c', v: 1 },
    { name: 'd', v: 0 },
  ];
  assert.deepEqual(PickerStats.sortRows(rows, 'v', 'asc').map(r => r.name), ['d', 'a', 'b', 'c']);
  assert.deepEqual(PickerStats.sortRows(rows, 'v', 'desc').map(r => r.name), ['a', 'b', 'c', 'd']);
});

test('sortRows sends nullish values last in both directions', () => {
  const rows = [
    { name: 'a', v: null },
    { name: 'b', v: 0.2 },
    { name: 'c', v: 0.8 },
    { name: 'd' },
  ];
  assert.deepEqual(PickerStats.sortRows(rows, 'v', 'desc').map(r => r.name), ['c', 'b', 'a', 'd']);
  assert.deepEqual(PickerStats.sortRows(rows, 'v', 'asc').map(r => r.name), ['b', 'c', 'a', 'd']);
});

test('sortRows returns a new array and never mutates the input', () => {
  const rows = PickerStats.metaRows(baseGen(), '3');
  const before = structuredClone(rows);
  PickerStats.sortRows(rows, 'topWr', 'desc');
  PickerStats.sortRows(rows, 'name', 'asc');
  assert.deepEqual(rows, before);
});

test('metaRows and pairLists leave G untouched and repeat identically', () => {
  const g = baseGen();
  const before = structuredClone(g);
  const rows1 = PickerStats.metaRows(g, '3');
  const pairs1 = PickerStats.pairLists(g, 0);
  const rows2 = PickerStats.metaRows(g, '3');
  const pairs2 = PickerStats.pairLists(g, 0);
  assert.deepEqual(g, before);
  assert.deepEqual(rows1, rows2);
  assert.deepEqual(pairs1, pairs2);
});
