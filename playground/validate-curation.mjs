// one-off: validate var/howdoiplay-curation fragments against the howdoiplay
// extraction and the config pool before merging into content.json
import { readFileSync, readdirSync } from 'node:fs';

const hdp = JSON.parse(readFileSync('ref/dota2/howdoiplay/howdoiplay.json', 'utf8'));
const cfg = JSON.parse(readFileSync('config.json', 'utf8'));
const pool = [...new Set(cfg.pool.map(e => e.slug))].sort();
const dir = 'var/howdoiplay-curation';
const files = readdirSync(dir).filter(f => f.endsWith('.json')).sort();
const errors = [];

// heroes already committed in curated.json are regenerated verbatim by
// sync-fragments; several predate the authoring gates (naga carries 8 lines),
// so the 3-6 line / 120 char / min-counter gates apply to new curation only.
// Ref resolution and the pool slug-set equality apply to every fragment.
let committed = { heroes: {} };
try { committed = JSON.parse(readFileSync('ref/dota2/howdoiplay/curated.json', 'utf8')); } catch {}

const fragSlugs = files.map(f => f.replace('.json', ''));
if (fragSlugs.join(' ') !== pool.join(' ')) {
  errors.push(`fragment slug set differs from pool:\n  fragments: ${fragSlugs.join(' ')}\n  pool:      ${pool.join(' ')}`);
}

for (const f of files) {
  const frag = JSON.parse(readFileSync(`${dir}/${f}`, 'utf8'));
  const slug = frag.slug ?? '(no slug)';
  const hero = hdp.heroes[frag.slug];
  if (!hero) { errors.push(`${slug}: no extraction entry`); continue; }
  const gated = !(slug in committed.heroes);
  if (gated && (!Array.isArray(frag.lines) || frag.lines.length < 3 || frag.lines.length > 6)) {
    errors.push(`${slug}: ${frag.lines?.length} lines, want 3-6`);
  }
  let counterTags = 0;
  for (const [i, l] of (frag.lines ?? []).entries()) {
    if (l.tag !== 'tip' && l.tag !== 'counter') errors.push(`${slug}[${i}]: bad tag '${l.tag}'`);
    if (l.tag === 'counter') counterTags++;
    if (typeof l.text !== 'string' || !l.text.trim()) errors.push(`${slug}[${i}]: empty text`);
    if (gated && typeof l.text === 'string' && l.text.length > 120) errors.push(`${slug}[${i}]: ${l.text.length} chars over 120`);
    const m = /^(tips|counters)\[(\d+)\](?:\.notes\[(\d+)\])?$/.exec(l.from ?? '');
    if (!m) { errors.push(`${slug}[${i}]: malformed from '${l.from}'`); continue; }
    const arr = hero[m[1]];
    const n = Number(m[2]);
    if (n >= arr.length) { errors.push(`${slug}[${i}]: ${l.from} out of range, ${m[1]} has ${arr.length}`); continue; }
    if (m[3] !== undefined) {
      const notes = arr[n].notes;
      if (!Array.isArray(notes) || Number(m[3]) >= notes.length) {
        errors.push(`${slug}[${i}]: ${l.from} notes out of range, entry has ${notes?.length ?? 0}`);
      }
    }
  }
  if (gated && counterTags < 1) errors.push(`${slug}: no counter-tagged line`);
}

if (errors.length) {
  console.error(`VALIDATION FAILED (${errors.length}):`);
  console.error(errors.join('\n'));
  process.exit(1);
}
const newCount = fragSlugs.filter(s => !(s in committed.heroes)).length;
console.log(`ok: ${files.length} fragments valid, refs resolve, ${newCount} new fragments pass the authoring gates`);
