# visage matchup table

source: https://de.dotabuff.com/heroes/visage/counters, scraped 2026-09-24 during patch 7.41f. All pool heroes are pulled from the German locale subdomain for one consistent live window: it serves the identical live table to the English page (English hero names, same 'This Month' filter, refreshed within minutes at scrape time), while English pages for several pool heroes serve stale CDN snapshots to this machine. Columns per ref/dota2/README.md: \dis%\ positive = the listed enemy beats the file hero. Sampled games per pair reach the 100k+ range on common heroes.

Overall winrate context: 51.89% over the recent OpenDota public window.

Sanity: mirror consistency checked across all pool pairs from the same scrape window, worst divergence 1.81 percentage points across 153 pairs.
Divergent pairs above 1: see make fetch-matchups output.

| enemy | dis% | wr% | matches |
| --- | --- | --- | --- |
| Bristleback | 4.35 | 49.26 | 9454 |
| Beastmaster | 3.74 | 54.07 | 3743 |
| Naga Siren | 3.67 | 49.93 | 3032 |
| Phantom Lancer | 3.09 | 46.71 | 19395 |
| Riki | 2.93 | 47.38 | 6806 |
| Sand King | 2.93 | 50.52 | 5703 |
| Batrider | 2.81 | 55.61 | 1737 |
| Meepo | 2.76 | 47.44 | 3286 |
| Clinkz | 2.66 | 50.23 | 7052 |
| Venomancer | 2.60 | 52.48 | 8405 |
| Lycan | 2.48 | 51.94 | 2426 |
| Marci | 2.43 | 50.48 | 6200 |
| Dark Willow | 2.37 | 51.62 | 9613 |
| Enchantress | 2.34 | 52.29 | 4280 |
| Anti-Mage | 2.26 | 50.38 | 16050 |
| Gyrocopter | 2.26 | 54.78 | 3518 |
| Kez | 2.21 | 55.32 | 7430 |
| Warlock | 2.10 | 51.11 | 7236 |
| Terrorblade | 2.08 | 53.73 | 9065 |
| Phantom Assassin | 2.02 | 49.44 | 17982 |
| Windranger | 1.83 | 52.25 | 21453 |
| Puck | 1.79 | 54.17 | 4602 |
| Underlord | 1.78 | 51.17 | 13686 |
| Disruptor | 1.76 | 50.75 | 9605 |
| Axe | 1.75 | 50.52 | 24343 |
| Jakiro | 1.63 | 52.39 | 10633 |
| Earthshaker | 1.60 | 50.64 | 20449 |
| Winter Wyvern | 1.51 | 51.17 | 10296 |
| Razor | 1.50 | 50.95 | 7878 |
| Death Prophet | 1.50 | 52.59 | 4853 |
| Crystal Maiden | 1.45 | 49.66 | 17534 |
| Largo | 1.39 | 53.37 | 3024 |
| Medusa | 1.38 | 51.68 | 3955 |
| Faceless Void | 1.31 | 52.55 | 11376 |
| Viper | 1.30 | 53.00 | 9748 |
| Pangolier | 1.25 | 54.98 | 5324 |
| Wraith King | 1.22 | 46.89 | 15344 |
| Broodmother | 1.21 | 51.14 | 3643 |
| Monkey King | 1.12 | 55.87 | 5983 |
| Chen | 1.12 | 56.46 | 820 |
| Shadow Demon | 1.11 | 56.40 | 3282 |
| Sniper | 1.07 | 51.59 | 25105 |
| Weaver | 1.03 | 54.27 | 6939 |
| Sven | 1.03 | 50.72 | 11967 |
| Bloodseeker | 0.94 | 50.31 | 5216 |
| Ursa | 0.91 | 54.06 | 7480 |
| Queen of Pain | 0.88 | 54.42 | 11162 |
| Troll Warlord | 0.87 | 50.09 | 3965 |
| Hoodwink | 0.84 | 54.24 | 19576 |
| Drow Ranger | 0.83 | 55.30 | 15208 |
| Chaos Knight | 0.73 | 50.41 | 6221 |
| Io | 0.71 | 53.47 | 7574 |
| Phoenix | 0.71 | 50.43 | 7825 |
| Alchemist | 0.71 | 54.94 | 4461 |
| Void Spirit | 0.65 | 52.28 | 5715 |
| Spirit Breaker | 0.61 | 50.10 | 21326 |
| Vengeful Spirit | 0.60 | 49.21 | 14220 |
| Kunkka | 0.58 | 52.60 | 5664 |
| Omniknight | 0.58 | 51.74 | 3369 |
| Dragon Knight | 0.54 | 50.68 | 8248 |
| Silencer | 0.54 | 51.59 | 11955 |
| Ringmaster | 0.49 | 53.60 | 7714 |
| Storm Spirit | 0.49 | 55.57 | 8931 |
| Tiny | 0.48 | 57.65 | 6305 |
| Muerta | 0.40 | 54.34 | 3977 |
| Undying | 0.39 | 51.87 | 19843 |
| Brewmaster | 0.34 | 50.92 | 2610 |
| Leshrac | 0.34 | 51.76 | 3317 |
| Mars | 0.33 | 54.99 | 5754 |
| Bounty Hunter | 0.31 | 48.98 | 12487 |
| Slark | 0.23 | 52.13 | 11884 |
| Enigma | 0.18 | 51.44 | 6167 |
| Timbersaw | 0.16 | 57.13 | 7100 |
| Treant Protector | 0.13 | 53.71 | 7782 |
| Luna | 0.12 | 52.05 | 15576 |
| Nature's Prophet | 0.06 | 59.49 | 13913 |
| Ember Spirit | 0.01 | 53.40 | 12086 |
| Lich | -0.02 | 50.05 | 14011 |
| Skywrath Mage | -0.06 | 52.53 | 16200 |
| Dark Seer | -0.08 | 53.50 | 8724 |
| Abaddon | -0.13 | 51.66 | 4452 |
| Centaur Warrunner | -0.21 | 52.63 | 10179 |
| Keeper of the Light | -0.26 | 53.13 | 10348 |
| Earth Spirit | -0.28 | 53.34 | 10051 |
| Tidehunter | -0.32 | 53.07 | 9948 |
| Tusk | -0.38 | 54.88 | 8336 |
| Doom | -0.41 | 55.55 | 8176 |
| Legion Commander | -0.47 | 50.32 | 18980 |
| Snapfire | -0.49 | 53.96 | 15227 |
| Huskar | -0.50 | 57.66 | 5857 |
| Mirana | -0.51 | 51.45 | 16917 |
| Slardar | -0.55 | 52.99 | 11141 |
| Spectre | -0.67 | 49.57 | 14815 |
| Ogre Magi | -0.75 | 52.68 | 20548 |
| Morphling | -0.82 | 55.74 | 5858 |
| Magnus | -0.84 | 54.29 | 15289 |
| Dazzle | -0.84 | 52.47 | 7295 |
| Primal Beast | -0.85 | 54.11 | 4297 |
| Pugna | -0.97 | 53.28 | 5642 |
| Lina | -0.99 | 54.29 | 29885 |
| Oracle | -0.99 | 53.27 | 5476 |
| Pudge | -1.00 | 52.23 | 42944 |
| Templar Assassin | -1.03 | 57.77 | 8583 |
| Shadow Shaman | -1.14 | 51.85 | 19784 |
| Lion | -1.15 | 54.51 | 34262 |
| Invoker | -1.18 | 53.39 | 29039 |
| Techies | -1.24 | 53.07 | 18183 |
| Ancient Apparition | -1.30 | 52.27 | 8514 |
| Clockwerk | -1.44 | 55.96 | 5751 |
| Grimstroke | -1.49 | 52.34 | 9430 |
| Nyx Assassin | -1.56 | 51.78 | 7138 |
| Night Stalker | -1.57 | 52.48 | 10818 |
| Witch Doctor | -1.61 | 52.14 | 20388 |
| Rubick | -1.62 | 54.37 | 26891 |
| Necrophos | -1.64 | 52.94 | 21086 |
| Dawnbreaker | -1.68 | 52.29 | 12938 |
| Zeus | -1.72 | 54.35 | 16761 |
| Lone Druid | -1.89 | 56.50 | 6094 |
| Shadow Fiend | -1.95 | 56.85 | 33049 |
| Tinker | -2.03 | 57.46 | 8392 |
| Bane | -2.17 | 55.10 | 6265 |
| Arc Warden | -2.56 | 52.78 | 7607 |
| Juggernaut | -2.65 | 52.92 | 21406 |
| Outworld Destroyer | -2.93 | 52.39 | 10864 |
| Lifestealer | -3.20 | 52.97 | 22673 |
| Elder Titan | -3.82 | 55.70 | 2122 |
