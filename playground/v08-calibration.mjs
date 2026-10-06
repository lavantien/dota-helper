// one-off: print vs/with win-rate bands for the v0.8 batch heroes from the
// committed synergy crawl, the same calibration view the tusk gates used
import { readFileSync } from 'node:fs';

const roster = JSON.parse(readFileSync('ref/dota2/synergy/raw/_roster.json', 'utf8'));
const byId = new Map(roster.heroes.map(h => [h.id, h.slug]));

for (const slug of ['ember-spirit', 'lina', 'sniper', 'snapfire', 'pudge']) {
  const raw = JSON.parse(readFileSync(`ref/dota2/synergy/raw/${slug}.json`, 'utf8'));
  const rows = sec => raw[sec]
    .filter(r => byId.has(r.heroId2))
    .map(r => ({ slug: byId.get(r.heroId2), n: r.matchCount, wr: r.winCount / r.matchCount }))
    .sort((a, b) => a.wr - b.wr);
  const overall = sec => {
    const t = raw[sec].reduce((a, r) => ({ n: a.n + r.matchCount, w: a.w + r.winCount }), { n: 0, w: 0 });
    return (100 * t.w / t.n).toFixed(1) + '% over ' + t.n;
  };
  const fmt = r => `${r.slug} ${(100 * r.wr).toFixed(1)}% over ${r.n}`;
  for (const sec of ['vs', 'with']) {
    console.log(`${slug} ${sec} overall ${overall(sec)}`);
    console.log('  worst:');
    for (const r of rows(sec).filter(r => r.n >= 90).slice(0, 10)) console.log('    ' + fmt(r));
    console.log('  best:');
    for (const r of rows(sec).filter(r => r.n >= 90).slice(-10).reverse()) console.log('    ' + fmt(r));
  }
}
