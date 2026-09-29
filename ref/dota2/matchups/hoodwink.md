# hoodwink matchup table

source: https://de.dotabuff.com/heroes/hoodwink/counters, scraped 2026-09-24 during patch 7.41f. All pool heroes are pulled from the German locale subdomain for one consistent live window: it serves the identical live table to the English page (English hero names, same 'This Month' filter, refreshed within minutes at scrape time), while English pages for several pool heroes serve stale CDN snapshots to this machine. Columns per ref/dota2/README.md: \dis%\ positive = the listed enemy beats the file hero. Sampled games per pair reach the 100k+ range on common heroes.

Overall winrate context: 47.03% over the recent OpenDota public window.

Sanity: mirror consistency checked across all pool pairs from the same scrape window, worst divergence 1.81 percentage points across 153 pairs.
Divergent pairs above 1: see make fetch-matchups output.

| enemy | dis% | wr% | matches |
| --- | --- | --- | --- |
| Timbersaw | 1.99 | 50.48 | 64738 |
| Mars | 1.24 | 49.01 | 53498 |
| Spirit Breaker | 1.24 | 43.85 | 204553 |
| Primal Beast | 1.23 | 46.66 | 36227 |
| Windranger | 1.01 | 47.78 | 212089 |
| Beastmaster | 0.95 | 52.13 | 27125 |
| Bane | 0.93 | 46.65 | 53045 |
| Gyrocopter | 0.88 | 51.28 | 29622 |
| Omniknight | 0.87 | 46.01 | 27088 |
| Batrider | 0.80 | 52.96 | 11644 |
| Tinker | 0.78 | 49.70 | 66729 |
| Drow Ranger | 0.77 | 50.41 | 149711 |
| Axe | 0.72 | 46.02 | 228732 |
| Puck | 0.69 | 50.31 | 41841 |
| Keeper of the Light | 0.64 | 46.87 | 86499 |
| Tiny | 0.62 | 52.84 | 59161 |
| Kez | 0.59 | 52.20 | 64991 |
| Void Spirit | 0.57 | 47.00 | 52615 |
| Shadow Demon | 0.54 | 52.22 | 27798 |
| Ogre Magi | 0.49 | 46.09 | 207429 |
| Queen of Pain | 0.46 | 49.78 | 111252 |
| Centaur Warrunner | 0.45 | 46.59 | 101694 |
| Bloodseeker | 0.45 | 45.25 | 44203 |
| Phantom Lancer | 0.43 | 43.38 | 160667 |
| Clockwerk | 0.39 | 49.03 | 53292 |
| Terrorblade | 0.36 | 50.47 | 73117 |
| Lifestealer | 0.33 | 44.06 | 224237 |
| Zeus | 0.28 | 47.12 | 166117 |
| Largo | 0.24 | 49.43 | 24333 |
| Doom | 0.23 | 49.88 | 74216 |
| Lina | 0.22 | 47.96 | 275111 |
| Enigma | 0.21 | 45.88 | 53211 |
| Ursa | 0.21 | 49.69 | 65769 |
| Treant Protector | 0.20 | 48.43 | 70446 |
| Juggernaut | 0.20 | 44.70 | 218280 |
| Earth Spirit | 0.19 | 47.55 | 95741 |
| Legion Commander | 0.16 | 44.04 | 172494 |
| Weaver | 0.14 | 50.12 | 60698 |
| Vengeful Spirit | 0.13 | 43.93 | 141152 |
| Ember Spirit | 0.13 | 48.02 | 115156 |
| Magnus | 0.12 | 48.13 | 146122 |
| Pugna | 0.09 | 46.84 | 48882 |
| Warlock | 0.08 | 47.80 | 69893 |
| Dragon Knight | 0.07 | 45.64 | 74942 |
| Ringmaster | 0.07 | 48.85 | 70305 |
| Bounty Hunter | 0.06 | 43.40 | 120878 |
| Clinkz | 0.03 | 47.49 | 60408 |
| Jakiro | -0.01 | 48.81 | 104497 |
| Tidehunter | -0.03 | 47.46 | 88207 |
| Sand King | -0.04 | 48.22 | 49691 |
| Wraith King | -0.05 | 42.23 | 143964 |
| Oracle | -0.05 | 46.92 | 46320 |
| Venomancer | -0.08 | 50.06 | 82273 |
| Leshrac | -0.12 | 46.81 | 23471 |
| Skywrath Mage | -0.13 | 47.25 | 153307 |
| Mirana | -0.13 | 45.54 | 177171 |
| Phantom Assassin | -0.14 | 45.98 | 167936 |
| Naga Siren | -0.16 | 48.53 | 19881 |
| Sven | -0.16 | 46.42 | 108182 |
| Kunkka | -0.17 | 48.07 | 50336 |
| Enchantress | -0.17 | 49.70 | 35576 |
| Razor | -0.19 | 47.26 | 72627 |
| Crystal Maiden | -0.21 | 45.69 | 177995 |
| Slark | -0.22 | 47.22 | 116251 |
| Nature's Prophet | -0.22 | 55.32 | 128768 |
| Viper | -0.22 | 49.35 | 88463 |
| Phoenix | -0.24 | 45.80 | 68439 |
| Broodmother | -0.24 | 47.19 | 24974 |
| Dawnbreaker | -0.26 | 45.31 | 119384 |
| Faceless Void | -0.27 | 48.90 | 102473 |
| Techies | -0.28 | 46.71 | 158270 |
| Nyx Assassin | -0.29 | 44.91 | 68366 |
| Arc Warden | -0.30 | 44.86 | 59279 |
| Sniper | -0.32 | 47.54 | 241642 |
| Snapfire | -0.32 | 48.57 | 146361 |
| Outworld Destroyer | -0.33 | 44.17 | 100729 |
| Lion | -0.34 | 48.57 | 340392 |
| Pangolier | -0.37 | 51.74 | 47994 |
| Disruptor | -0.37 | 47.49 | 96494 |
| Storm Spirit | -0.40 | 51.58 | 83942 |
| Undying | -0.40 | 47.24 | 195333 |
| Huskar | -0.44 | 52.84 | 45725 |
| Underlord | -0.45 | 48.01 | 140042 |
| Medusa | -0.46 | 48.25 | 32464 |
| Chen | -0.49 | 53.39 | 5561 |
| Death Prophet | -0.50 | 49.43 | 41668 |
| Io | -0.51 | 49.57 | 66248 |
| Grimstroke | -0.51 | 45.80 | 84187 |
| Muerta | -0.51 | 50.18 | 34431 |
| Marci | -0.52 | 48.11 | 50151 |
| Tusk | -0.52 | 49.96 | 77287 |
| Necrophos | -0.54 | 46.46 | 197706 |
| Winter Wyvern | -0.54 | 47.82 | 93626 |
| Troll Warlord | -0.56 | 46.02 | 30570 |
| Dark Seer | -0.57 | 48.76 | 75112 |
| Earthshaker | -0.57 | 47.29 | 189752 |
| Dazzle | -0.58 | 46.75 | 61958 |
| Abaddon | -0.60 | 46.69 | 37036 |
| Morphling | -0.60 | 50.52 | 47183 |
| Luna | -0.63 | 47.40 | 132020 |
| Lich | -0.64 | 44.97 | 139052 |
| Alchemist | -0.65 | 51.35 | 35154 |
| Templar Assassin | -0.70 | 52.71 | 70076 |
| Witch Doctor | -0.73 | 45.76 | 204540 |
| Slardar | -0.75 | 47.88 | 105282 |
| Brewmaster | -0.76 | 46.51 | 19435 |
| Riki | -0.77 | 45.45 | 58809 |
| Meepo | -0.77 | 45.32 | 22316 |
| Lone Druid | -0.83 | 50.42 | 50741 |
| Elder Titan | -0.85 | 47.33 | 13543 |
| Night Stalker | -0.86 | 46.26 | 98231 |
| Invoker | -0.87 | 47.78 | 263176 |
| Monkey King | -0.90 | 53.11 | 53406 |
| Pudge | -0.92 | 46.71 | 396241 |
| Silencer | -0.93 | 47.68 | 117128 |
| Chaos Knight | -0.94 | 46.60 | 52586 |
| Shadow Shaman | -0.94 | 46.13 | 195909 |
| Spectre | -0.97 | 44.11 | 146235 |
| Ancient Apparition | -1.03 | 46.53 | 79728 |
| Rubick | -1.09 | 48.62 | 270325 |
| Shadow Fiend | -1.14 | 51.19 | 296700 |
| Dark Willow | -1.21 | 50.00 | 95035 |
| Visage | -1.41 | 45.76 | 19576 |
| Lycan | -1.72 | 51.13 | 14787 |
| Anti-Mage | -1.95 | 49.16 | 128103 |
| Bristleback | -2.56 | 50.91 | 78161 |
