import { test } from 'node:test';
import assert from 'node:assert/strict';
import PickerTabs from './picker-tabs.js';

const resolveTab = PickerTabs.resolveTab;

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
