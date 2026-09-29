// one-off: merge validated curation fragments into content.json as
// per-hero "mechanics" arrays (inserted after "tech", formatting preserved by
// text-level insertion) and emit ref/dota2/howdoiplay/curated.json provenance
import { readFileSync, writeFileSync, readdirSync } from 'node:fs';

const contentPath = 'content.json';
let text = readFileSync(contentPath, 'utf8');
const hdp = JSON.parse(readFileSync('ref/dota2/howdoiplay/howdoiplay.json', 'utf8'));
const dir = 'var/howdoiplay-curation';
const files = readdirSync(dir).filter(f => f.endsWith('.json')).sort();

// find the close bracket of the array that opens at openIdx, honoring string
// literals so prose brackets cannot fool the scan
function matchBracket(s, openIdx) {
  let depth = 0, inStr = false;
  for (let i = openIdx; i < s.length; i++) {
    const c = s[i];
    if (inStr) {
      if (c === '\\') i++;
      else if (c === '"') inStr = false;
    } else if (c === '"') inStr = true;
    else if (c === '[') depth++;
    else if (c === ']') { depth--; if (depth === 0) return i; }
  }
  throw new Error(`unmatched bracket at ${openIdx}`);
}

// same scan for braces, bounding a hero object
function matchBrace(s, openIdx) {
  let depth = 0, inStr = false;
  for (let i = openIdx; i < s.length; i++) {
    const c = s[i];
    if (inStr) {
      if (c === '\\') i++;
      else if (c === '"') inStr = false;
    } else if (c === '"') inStr = true;
    else if (c === '{') depth++;
    else if (c === '}') { depth--; if (depth === 0) return i; }
  }
  throw new Error(`unmatched brace at ${openIdx}`);
}

const before = JSON.parse(text);
// prior verdicts ride through a re-merge: a line keeps its verified block from
// the committed curated.json when the committed line is unchanged, unless the
// fragment carries a fresh verdict (new or re-verified lines)
let prev = { heroes: {} };
try {
  prev = JSON.parse(readFileSync('ref/dota2/howdoiplay/curated.json', 'utf8'));
} catch {}
const curated = {
  curatedAt: prev.curatedAt ?? new Date().toISOString().slice(0, 10),
  source: 'ref/dota2/howdoiplay/howdoiplay.json',
  patch: hdp.patch,
  heroes: {},
};

for (const f of files) {
  const frag = JSON.parse(readFileSync(`${dir}/${f}`, 'utf8'));
  const heroAt = text.indexOf(`"${frag.slug}": {`);
  if (heroAt < 0) throw new Error(`${frag.slug}: hero block not found`);
  const heroOpen = text.indexOf('{', heroAt);
  const heroClose = matchBrace(text, heroOpen);
  const techAt = text.indexOf('"tech": [', heroAt);
  if (techAt < 0 || techAt > heroClose) throw new Error(`${frag.slug}: tech array not found in block`);
  const techClose = matchBracket(text, techAt + text.slice(techAt).indexOf('['));
  // one inline line per hero array, timings style, keeps content.json under the SLOC cap
  const lines = frag.lines.map(l => `["${l.tag}", ${JSON.stringify(l.text)}]`).join(', ');
  // replace the mechanics array wherever it sits inside this hero block, or
  // insert one after tech; the block bounds keep the match on this hero
  const mechKey = /"mechanics"\s*:/.exec(text.slice(heroOpen, heroClose + 1));
  if (mechKey) {
    const open = heroOpen + text.slice(heroOpen, heroClose + 1).indexOf('[', mechKey.index);
    const mechClose = matchBracket(text, open);
    text = text.slice(0, open + 1) + lines + text.slice(mechClose);
  } else {
    const block = `,\n      "mechanics": [${lines}]`;
    text = text.slice(0, techClose + 1) + block + text.slice(techClose + 1);
  }
  // a second mechanics key would silently win or lose in JSON.parse, refuse it
  const blockNow = text.slice(heroOpen, matchBrace(text, heroOpen) + 1);
  if ((blockNow.match(/"mechanics"\s*:/g) || []).length !== 1) {
    throw new Error(`${frag.slug}: hero block carries more than one mechanics key`);
  }
  const prevEntries = prev.heroes[frag.slug] ?? [];
  // drop accounting is keyed on from refs, so a duplicate ref anywhere would
  // let a dropped line hide behind its twin: refuse the shape outright
  const fragRefs = frag.lines.map(l => l.from);
  if (new Set(fragRefs).size !== fragRefs.length) {
    throw new Error(`${frag.slug}: fragment carries duplicate from refs, drop accounting cannot track them`);
  }
  const prevRefs = prevEntries.map(pe => pe && pe.from).filter(Boolean);
  if (new Set(prevRefs).size !== prevRefs.length) {
    throw new Error(`${frag.slug}: committed provenance carries duplicate from refs, resolve before merging`);
  }
  // change protocol: every line carries a verified block whose text field is
  // the exact mechanics text it covers. a line inherits the committed verdict
  // only when the committed line is unchanged in tag, from ref, and text; any
  // new, re-tagged, re-attributed, or reworded line needs a fresh block with
  // matching text, otherwise the merge aborts loudly. this is what stops a
  // stale or edited fragment from silently reverting verified rewordings,
  // resurrecting dropped lines, or keeping stale verdicts.
  const prevMech = (before.heroes[frag.slug] && before.heroes[frag.slug].mechanics) || [];
  const sameShape = prevMech.length === frag.lines.length;
  const missing = [];
  curated.heroes[frag.slug] = frag.lines.map((l, idx) => {
    const e = { idx, from: l.from };
    let v = l.verified;
    const prevEntry = prevEntries[idx];
    if (!v && sameShape && prevMech[idx] && prevEntry &&
        prevEntry.from === l.from && prevMech[idx][0] === l.tag && prevMech[idx][1] === l.text) {
      v = prevEntry.verified;
    }
    if (!v) missing.push(`${frag.slug} line ${idx} (${l.from}) changes committed mechanics without a fresh verified block`);
    else if (v.text !== l.text) missing.push(`${frag.slug} line ${idx} (${l.from}) text differs from the text its verdict covers, re-verify and restamp`);
    // a re-tagged or re-pointed line riding the byte-identical committed
    // verdict is a stale carryover, not a fresh verification; so is a reworded
    // line whose verdict keeps the committed date, verdict, and source, a text
    // patch away from the committed block, the cited source never verified the
    // new wording
    else if (prevEntry && prevEntry.verified && prevMech[idx] &&
        JSON.stringify(v) === JSON.stringify(prevEntry.verified) &&
        (prevMech[idx][0] !== l.tag || prevEntry.from !== l.from)) {
      missing.push(`${frag.slug} line ${idx} (${l.from}) re-tags or re-points a committed line while carrying its identical verdict block, re-verify and restamp`);
    }
    else if (prevEntry && prevEntry.verified && prevMech[idx] && prevMech[idx][1] !== l.text &&
        v.verdict === prevEntry.verified.verdict && v.source === prevEntry.verified.source &&
        v.date === prevEntry.verified.date) {
      missing.push(`${frag.slug} line ${idx} (${l.from}) rewords a committed line while carrying its date, verdict, and source unchanged, re-verify and restamp`);
    }
    if (v) e.verified = v;
    return e;
  });
  // deletions must be deliberate: every committed from ref missing from the
  // fragment is accounted in the fragment's own "dropped" array, nothing
  // silently disappears
  const keptRefs = frag.lines.map(l => l.from);
  const removedRefs = prevEntries
    .map(pe => (pe && pe.from && !keptRefs.includes(pe.from)) ? pe.from : null)
    .filter(Boolean)
    .sort();
  const declaredDrops = [...(frag.dropped ?? [])].sort().join(',');
  if (removedRefs.join(',') !== declaredDrops) {
    missing.push(`${frag.slug}: committed mechanics (${removedRefs.join(', ') || 'none'}) missing from the fragment are not accounted by its dropped array (${declaredDrops || 'none'}), record intentional deletions there`);
  }
  if (missing.length) throw new Error(missing.join('\n'));
}

// semantic verification: parsed result equals parsed input plus mechanics
const after = JSON.parse(text);
for (const slug of Object.keys(after.heroes)) {
  const frag = JSON.parse(readFileSync(`${dir}/${slug}.json`, 'utf8'));
  const want = (frag.lines ?? []).map(l => [l.tag, l.text]);
  if (JSON.stringify(after.heroes[slug].mechanics) !== JSON.stringify(want)) {
    throw new Error(`${slug}: merged mechanics differ from fragment`);
  }
  const stripped = { ...after.heroes[slug] };
  delete stripped.mechanics;
  const beforeStripped = { ...before.heroes[slug] };
  delete beforeStripped.mechanics;
  if (JSON.stringify(stripped) !== JSON.stringify(beforeStripped)) {
    throw new Error(`${slug}: merge mutated existing prose`);
  }
}
if (Object.keys(curated.heroes).length !== files.length) throw new Error('curated hero count mismatch');

// curatedAt records the last content change, not the last run: a no-op
// re-merge keeps the committed stamp instead of minting a fresh diff
const prevHeroesRaw = JSON.stringify(prev.heroes ?? {});
if (prevHeroesRaw !== JSON.stringify(curated.heroes) || prev.patch !== hdp.patch) {
  curated.curatedAt = new Date().toISOString().slice(0, 10);
}
const curatedText = JSON.stringify(curated, null, 2) + '\n';
let diskCurated = '';
try { diskCurated = readFileSync('ref/dota2/howdoiplay/curated.json', 'utf8'); } catch {}
const contentChanged = text !== readFileSync(contentPath, 'utf8');
const curatedChanged = curatedText !== diskCurated;
if (contentChanged) writeFileSync(contentPath, text, 'utf8');
if (curatedChanged) writeFileSync('ref/dota2/howdoiplay/curated.json', curatedText, 'utf8');
console.log(`ok: merged mechanics for ${files.length} heroes (patch ${hdp.patch}), ${contentChanged || curatedChanged ? 'wrote changes' : 'no changes'}`);
