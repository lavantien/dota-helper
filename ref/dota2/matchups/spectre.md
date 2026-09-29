# spectre matchup table

source: https://de.dotabuff.com/heroes/spectre/counters, scraped 2026-09-24 during patch 7.41f. All pool heroes are pulled from the German locale subdomain for one consistent live window: it serves the identical live table to the English page (English hero names, same 'This Month' filter, refreshed within minutes at scrape time), while English pages for several pool heroes serve stale CDN snapshots to this machine. Columns per ref/dota2/README.md: \dis%\ positive = the listed enemy beats the file hero. Sampled games per pair reach the 100k+ range on common heroes.

Overall winrate context: 52.79% over the recent OpenDota public window.

Sanity: mirror consistency checked across all pool pairs from the same scrape window, worst divergence 1.81 percentage points across 153 pairs.
Divergent pairs above 1: see make fetch-matchups output.

| enemy | dis% | wr% | matches |
| --- | --- | --- | --- |
| Undying | 5.37 | 48.32 | 140704 |
| Meepo | 4.64 | 46.53 | 17182 |
| Primal Beast | 4.28 | 50.30 | 27287 |
| Io | 4.01 | 51.61 | 45292 |
| Chen | 3.83 | 55.41 | 4032 |
| Enchantress | 3.64 | 52.45 | 26513 |
| Lycan | 3.63 | 52.21 | 11961 |
| Naga Siren | 3.54 | 51.43 | 17280 |
| Necrophos | 3.22 | 49.60 | 186798 |
| Largo | 3.12 | 53.14 | 18018 |
| Invoker | 2.97 | 50.87 | 199894 |
| Medusa | 2.92 | 51.51 | 32863 |
| Phantom Lancer | 2.91 | 47.80 | 130903 |
| Kez | 2.85 | 56.53 | 51465 |
| Bristleback | 2.83 | 52.12 | 70822 |
| Phoenix | 2.73 | 49.59 | 48005 |
| Ember Spirit | 2.61 | 52.29 | 92271 |
| Centaur Warrunner | 2.59 | 51.22 | 93160 |
| Beastmaster | 2.52 | 57.11 | 21065 |
| Lifestealer | 2.48 | 48.86 | 234759 |
| Tinker | 2.44 | 54.80 | 61873 |
| Abaddon | 2.38 | 50.38 | 32931 |
| Underlord | 2.26 | 52.09 | 125512 |
| Warlock | 2.17 | 52.44 | 63320 |
| Brewmaster | 2.07 | 50.39 | 15322 |
| Earth Spirit | 2.07 | 52.43 | 71028 |
| Keeper of the Light | 1.90 | 52.41 | 65778 |
| Broodmother | 1.88 | 51.79 | 19035 |
| Crystal Maiden | 1.83 | 50.50 | 152327 |
| Pangolier | 1.80 | 56.17 | 38199 |
| Lone Druid | 1.79 | 54.47 | 44897 |
| Leshrac | 1.78 | 51.63 | 19150 |
| Marci | 1.72 | 52.57 | 39220 |
| Dawnbreaker | 1.68 | 50.21 | 96845 |
| Treant Protector | 1.63 | 53.75 | 54844 |
| Elder Titan | 1.58 | 51.57 | 11091 |
| Nyx Assassin | 1.57 | 49.83 | 62167 |
| Dark Seer | 1.56 | 53.34 | 56537 |
| Tidehunter | 1.54 | 52.66 | 80832 |
| Queen of Pain | 1.52 | 55.47 | 102244 |
| Weaver | 1.47 | 55.50 | 55648 |
| Storm Spirit | 1.44 | 56.40 | 69278 |
| Terrorblade | 1.43 | 56.12 | 59413 |
| Alchemist | 1.41 | 55.95 | 31949 |
| Anti-Mage | 1.40 | 52.59 | 130455 |
| Phantom Assassin | 1.34 | 51.35 | 165520 |
| Pudge | 1.27 | 51.50 | 308279 |
| Bounty Hunter | 1.24 | 49.11 | 97229 |
| Viper | 1.18 | 54.70 | 84385 |
| Witch Doctor | 1.13 | 50.78 | 170145 |
| Slark | 1.12 | 52.68 | 127702 |
| Jakiro | 1.09 | 54.46 | 90989 |
| Oracle | 1.06 | 52.62 | 40696 |
| Timbersaw | 1.04 | 58.16 | 51940 |
| Tiny | 0.98 | 59.17 | 51945 |
| Kunkka | 0.97 | 53.68 | 46318 |
| Slardar | 0.92 | 52.98 | 96562 |
| Hoodwink | 0.87 | 55.89 | 146338 |
| Morphling | 0.82 | 55.78 | 38933 |
| Shadow Demon | 0.77 | 58.71 | 24431 |
| Venomancer | 0.77 | 55.95 | 74415 |
| Visage | 0.75 | 50.38 | 14836 |
| Dark Willow | 0.74 | 54.77 | 76102 |
| Shadow Fiend | 0.70 | 56.21 | 256957 |
| Bloodseeker | 0.69 | 51.83 | 39903 |
| Skywrath Mage | 0.67 | 53.27 | 136474 |
| Wraith King | 0.57 | 48.45 | 144140 |
| Omniknight | 0.55 | 53.17 | 24601 |
| Chaos Knight | 0.54 | 51.86 | 49619 |
| Void Spirit | 0.53 | 53.85 | 43080 |
| Juggernaut | 0.29 | 51.45 | 225978 |
| Tusk | 0.26 | 55.90 | 61980 |
| Batrider | 0.25 | 60.18 | 9506 |
| Dazzle | 0.24 | 52.76 | 52118 |
| Techies | 0.24 | 53.02 | 120091 |
| Clockwerk | 0.23 | 55.97 | 45079 |
| Sven | 0.20 | 52.87 | 94128 |
| Snapfire | 0.16 | 54.89 | 122727 |
| Pugna | 0.14 | 53.60 | 45222 |
| Troll Warlord | 0.13 | 52.10 | 29605 |
| Spirit Breaker | 0.05 | 51.87 | 181150 |
| Gyrocopter | 0.05 | 58.90 | 27933 |
| Arc Warden | -0.02 | 51.47 | 48864 |
| Monkey King | -0.10 | 59.03 | 50431 |
| Mirana | -0.16 | 52.41 | 153943 |
| Ogre Magi | -0.20 | 53.56 | 185286 |
| Sand King | -0.24 | 55.22 | 46293 |
| Ringmaster | -0.25 | 55.97 | 54848 |
| Lich | -0.35 | 51.55 | 118579 |
| Huskar | -0.40 | 59.51 | 41513 |
| Luna | -0.45 | 54.04 | 120464 |
| Earthshaker | -0.46 | 53.98 | 166628 |
| Outworld Destroyer | -0.54 | 51.24 | 92751 |
| Disruptor | -0.55 | 54.49 | 91569 |
| Lina | -0.58 | 55.49 | 239120 |
| Zeus | -0.61 | 54.81 | 159889 |
| Magnus | -0.64 | 55.69 | 123976 |
| Razor | -0.66 | 54.54 | 68261 |
| Faceless Void | -0.71 | 56.16 | 101836 |
| Windranger | -0.71 | 56.23 | 190553 |
| Nature's Prophet | -0.74 | 62.59 | 124195 |
| Doom | -0.77 | 57.71 | 68468 |
| Rubick | -0.91 | 55.23 | 227745 |
| Mars | -0.95 | 58.04 | 44370 |
| Legion Commander | -1.02 | 52.05 | 164450 |
| Riki | -1.19 | 52.71 | 58926 |
| Puck | -1.19 | 58.99 | 33538 |
| Death Prophet | -1.26 | 57.02 | 40387 |
| Clinkz | -1.27 | 55.66 | 54739 |
| Silencer | -1.27 | 54.82 | 121650 |
| Winter Wyvern | -1.53 | 55.70 | 83495 |
| Vengeful Spirit | -1.56 | 52.50 | 128770 |
| Lion | -1.65 | 56.56 | 292286 |
| Bane | -1.68 | 56.26 | 46400 |
| Night Stalker | -1.97 | 54.25 | 89751 |
| Enigma | -2.07 | 55.12 | 44176 |
| Drow Ranger | -2.18 | 60.19 | 155524 |
| Grimstroke | -2.23 | 54.46 | 69702 |
| Ursa | -2.24 | 58.99 | 62251 |
| Dragon Knight | -2.32 | 54.91 | 75725 |
| Muerta | -2.44 | 58.97 | 31246 |
| Axe | -2.60 | 56.03 | 218707 |
| Ancient Apparition | -2.62 | 55.01 | 84449 |
| Shadow Shaman | -2.84 | 54.83 | 178522 |
| Templar Assassin | -2.97 | 61.80 | 61058 |
| Sniper | -4.59 | 58.37 | 309626 |
