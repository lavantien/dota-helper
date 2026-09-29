import { readFileSync } from "node:fs";

const dir = new URL("../ref/dota2/synergy/raw/", import.meta.url);
const rosterRaw = JSON.parse(readFileSync(new URL("_roster.json", dir), "utf8"));
const rosterList = Array.isArray(rosterRaw) ? rosterRaw : rosterRaw.heroes;
const byId = new Map();
for (const h of rosterList) byId.set(h.heroId ?? h.id, h);

const wr = (r) => +((r.winCount / r.matchCount) * 100).toFixed(1);

for (const slug of ["abaddon", "primal-beast"]) {
  const c = JSON.parse(readFileSync(new URL(`${slug}.json`, dir), "utf8"));
  console.log(`\n=== ${slug} (${c.name}) keys=[${Object.keys(c).join(",")}] fetched ${c.fetchedAt}`);
  for (const key of ["vs", "with"]) {
    const rows = c[key] ?? [];
    console.log(`-- ${key}: ${rows.length} rows, sample: ${JSON.stringify(rows[0])}`);
    const named = rows
      .map((r) => ({ ...r, hero: byId.get(r.heroId2)?.slug ?? `id${r.heroId2}`, wr: wr(r) }))
      .filter((r) => r.matchCount >= 30);
    const asc = [...named].sort((a, b) => a.wr - b.wr);
    console.log("  lowest wr:");
    for (const r of asc.slice(0, 10)) console.log(`    ${r.hero} ${r.wr} on ${r.matchCount} (syn ${r.synergy})`);
    console.log("  highest wr:");
    for (const r of asc.slice(-10).reverse()) console.log(`    ${r.hero} ${r.wr} on ${r.matchCount} (syn ${r.synergy})`);
  }
}

const popRaw = readFileSync(new URL("_popularity.json", dir), "utf8");
for (const line of popRaw.split(/[\n{},]/)) {
  if (/abaddon|primal/.test(line)) console.log("pop:", line.trim());
}
