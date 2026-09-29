// one-off tamper test for the merge-mechanics verdict guard. three paths:
// a stripped fragment (no verified blocks) inherits committed verdicts only
// when tag, from ref, and text all match the committed line; a line carrying
// its own verdict block may not re-tag or re-point a committed line while
// riding the byte-identical committed verdict, nor reword one while keeping
// the committed date, verdict, and source with only the verdict text patched;
// deleting committed lines requires the fragment's dropped array to account
// exactly what went missing. guard trips abort before any write, but the
// accounted-deletion path writes, so every run snapshots and restores
// content.json and curated.json too.
import { readFileSync, writeFileSync } from 'node:fs';
import { spawnSync } from 'node:child_process';

const fragPath = 'var/howdoiplay-curation/bloodseeker.json';
const writtenPaths = ['content.json', 'ref/dota2/howdoiplay/curated.json'];
const real = readFileSync(fragPath, 'utf8');
const frag = JSON.parse(real);
// simulate a regenerated stale fragment: no verdicts anywhere
const stripped = { slug: frag.slug, lines: frag.lines.map(({ tag, text, from }) => ({ tag, text, from })) };
const editLine = (lines, i, patch) => lines.map((l, idx) => (idx === i ? { ...l, ...patch } : l));

function runMerge(label, fragment, expectAbort) {
  writeFileSync(fragPath, JSON.stringify(fragment, null, 2) + '\n', 'utf8');
  const snap = writtenPaths.map(p => [p, readFileSync(p, 'utf8')]);
  const r = spawnSync('node', ['playground/merge-mechanics.mjs'], { encoding: 'utf8' });
  const aborted = r.status !== 0;
  const out = `${r.stdout}${r.stderr}`;
  for (const [p, bytes] of snap) writeFileSync(p, bytes, 'utf8');
  if (expectAbort && !aborted) {
    console.error(`FAIL ${label}: merge did not abort\n${out}`);
    process.exit(1);
  }
  if (!expectAbort && aborted) {
    console.error(`FAIL ${label}: clean merge aborted\n${out}`);
    process.exit(1);
  }
  console.log(aborted ? `ok: ${label} aborted -> ${out.trim().split('\n').find(l => l.includes('line 5') || l.includes('line 2') || l.includes('dropped')) || out.trim().split('\n')[0]}`
                      : `ok: ${label} -> ${out.trim().split('\n').pop()}`);
}

try {
  // inheritance path: unchanged lines ride through, any mutation aborts
  runMerge('clean carryover', stripped, false);
  runMerge('tag flip', { ...stripped, lines: editLine(stripped.lines, 5, { tag: 'tip' }) }, true);
  runMerge('from swap', { ...stripped, lines: editLine(stripped.lines, 5, { from: 'counters[2]' }) }, true);
  runMerge('text reword', { ...stripped, lines: editLine(stripped.lines, 5, { text: stripped.lines[5].text + ' tampered' }) }, true);
  // fresh-verdict path: self-carried verdicts may not be stale carryovers
  runMerge('fresh verdict from re-point', { slug: frag.slug, lines: editLine(frag.lines, 2, { from: 'tips[1]' }) }, true);
  runMerge('fresh verdict tag flip', { slug: frag.slug, lines: editLine(frag.lines, 6, { tag: 'tip' }) }, true);
  // reworded line riding the committed verdict with only its text patched:
  // date, verdict, and source unchanged means no fresh verification happened
  runMerge('patched verdict text reword', { slug: frag.slug, lines: editLine(frag.lines, 5, {
    text: frag.lines[5].text + ' tampered',
    verified: { ...frag.lines[5].verified, text: frag.lines[5].text + ' tampered' },
  }) }, true);
  // deletion path: dropping committed lines must be accounted in dropped
  runMerge('unaccounted deletion', { slug: frag.slug, lines: frag.lines.slice(0, -1) }, true);
  runMerge('accounted deletion', { slug: frag.slug, lines: frag.lines.slice(0, -1), dropped: ['counters[4]'] }, false);
  // duplicate from refs would let a dropped line hide behind its twin
  runMerge('duplicate from ref', { slug: frag.slug, lines: editLine(frag.lines, 4, { from: 'counters[1]' }) }, true);
} finally {
  writeFileSync(fragPath, real, 'utf8');
}
// real fragment back in place: merge is a no-op
runMerge('real fragment restore', JSON.parse(real), false);
