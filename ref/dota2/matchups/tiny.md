# tiny matchup table

source: https://de.dotabuff.com/heroes/tiny/counters, scraped 2026-09-24 during patch 7.41f. All pool heroes are pulled from the German locale subdomain for one consistent live window: it serves the identical live table to the English page (English hero names, same 'This Month' filter, refreshed within minutes at scrape time), while English pages for several pool heroes serve stale CDN snapshots to this machine. Columns per ref/dota2/README.md: \dis%\ positive = the listed enemy beats the file hero. Sampled games per pair reach the 100k+ range on common heroes.

Overall winrate context: 45% over the recent OpenDota public window.

Sanity: mirror consistency checked across all pool pairs from the same scrape window, worst divergence 1.81 percentage points across 153 pairs.
Divergent pairs above 1: see make fetch-matchups output.

| enemy | dis% | wr% | matches |
| --- | --- | --- | --- |
| Lifestealer | 3.53 | 38.38 | 86028 |
| Necrophos | 2.38 | 40.83 | 74682 |
| Timbersaw | 1.99 | 47.14 | 26259 |
| Underlord | 1.87 | 42.77 | 50321 |
| Undying | 1.81 | 42.21 | 60099 |
| Huskar | 1.63 | 47.37 | 19590 |
| Templar Assassin | 1.41 | 47.20 | 24869 |
| Viper | 1.39 | 44.64 | 35815 |
| Ogre Magi | 1.38 | 42.37 | 75493 |
| Vengeful Spirit | 1.33 | 40.10 | 50030 |
| Venomancer | 1.27 | 45.54 | 29628 |
| Kez | 1.26 | 48.08 | 21577 |
| Wraith King | 1.00 | 38.66 | 53100 |
| Ember Spirit | 0.98 | 44.17 | 39182 |
| Queen of Pain | 0.97 | 46.09 | 41955 |
| Clockwerk | 0.94 | 45.36 | 19038 |
| Disruptor | 0.91 | 43.26 | 36551 |
| Terrorblade | 0.90 | 46.66 | 25111 |
| Dazzle | 0.88 | 42.43 | 19199 |
| Razor | 0.87 | 43.24 | 28281 |
| Sniper | 0.85 | 43.49 | 94305 |
| Clinkz | 0.78 | 43.75 | 22358 |
| Sven | 0.76 | 42.63 | 40246 |
| Winter Wyvern | 0.76 | 43.57 | 30769 |
| Nature's Prophet | 0.67 | 50.81 | 48442 |
| Witch Doctor | 0.66 | 41.66 | 66738 |
| Rubick | 0.66 | 44.00 | 94622 |
| Slardar | 0.62 | 43.54 | 40154 |
| Phoenix | 0.57 | 42.21 | 19952 |
| Dawnbreaker | 0.50 | 41.81 | 38892 |
| Gyrocopter | 0.46 | 48.39 | 13070 |
| Enchantress | 0.45 | 45.91 | 13358 |
| Dragon Knight | 0.45 | 42.41 | 30616 |
| Earth Spirit | 0.42 | 44.35 | 31193 |
| Weaver | 0.42 | 46.64 | 24537 |
| Warlock | 0.42 | 44.46 | 24095 |
| Death Prophet | 0.38 | 45.43 | 16768 |
| Ringmaster | 0.37 | 45.46 | 22709 |
| Alchemist | 0.33 | 47.11 | 14781 |
| Batrider | 0.30 | 50.00 | 4608 |
| Treant Protector | 0.29 | 45.30 | 21883 |
| Zeus | 0.28 | 44.15 | 61640 |
| Mars | 0.21 | 46.88 | 20288 |
| Lich | 0.21 | 41.44 | 45136 |
| Crystal Maiden | 0.20 | 42.49 | 63631 |
| Shadow Fiend | 0.16 | 46.78 | 101273 |
| Sand King | 0.15 | 44.97 | 19609 |
| Doom | 0.14 | 46.78 | 28936 |
| Jakiro | 0.09 | 45.63 | 37871 |
| Grimstroke | 0.09 | 42.44 | 26253 |
| Luna | 0.06 | 43.78 | 46743 |
| Mirana | 0.06 | 42.56 | 62588 |
| Outworld Destroyer | 0.01 | 41.15 | 36740 |
| Lina | -0.03 | 45.17 | 95900 |
| Windranger | -0.05 | 45.73 | 78174 |
| Pudge | -0.09 | 43.11 | 118874 |
| Silencer | -0.09 | 43.89 | 45202 |
| Monkey King | -0.13 | 48.94 | 22375 |
| Io | -0.15 | 46.07 | 19442 |
| Snapfire | -0.16 | 45.38 | 50423 |
| Faceless Void | -0.17 | 45.71 | 39791 |
| Pangolier | -0.17 | 48.20 | 16968 |
| Shadow Demon | -0.23 | 49.59 | 10512 |
| Enigma | -0.24 | 43.50 | 18859 |
| Shadow Shaman | -0.25 | 42.66 | 69655 |
| Spectre | -0.32 | 40.83 | 51945 |
| Techies | -0.33 | 43.89 | 49275 |
| Centaur Warrunner | -0.34 | 44.45 | 37309 |
| Hoodwink | -0.36 | 47.16 | 59207 |
| Magnus | -0.39 | 45.59 | 49683 |
| Tinker | -0.40 | 47.67 | 21819 |
| Bristleback | -0.41 | 45.61 | 33682 |
| Ancient Apparition | -0.41 | 43.05 | 30383 |
| Marci | -0.42 | 44.99 | 17862 |
| Oracle | -0.44 | 44.41 | 15503 |
| Medusa | -0.49 | 45.25 | 12052 |
| Bounty Hunter | -0.50 | 41.35 | 41093 |
| Brewmaster | -0.51 | 43.42 | 6681 |
| Lycan | -0.54 | 46.71 | 5798 |
| Troll Warlord | -0.56 | 43.14 | 13450 |
| Void Spirit | -0.58 | 45.20 | 18862 |
| Chaos Knight | -0.63 | 43.39 | 21490 |
| Kunkka | -0.63 | 45.51 | 19438 |
| Keeper of the Light | -0.63 | 45.21 | 27911 |
| Chen | -0.64 | 50.07 | 2033 |
| Lone Druid | -0.65 | 47.05 | 17651 |
| Storm Spirit | -0.66 | 48.51 | 28906 |
| Bane | -0.66 | 45.32 | 17697 |
| Juggernaut | -0.67 | 42.75 | 83424 |
| Night Stalker | -0.69 | 43.27 | 34262 |
| Visage | -0.73 | 42.35 | 6312 |
| Largo | -0.79 | 47.32 | 8895 |
| Muerta | -0.85 | 47.35 | 13758 |
| Dark Seer | -0.86 | 45.99 | 25816 |
| Tusk | -0.88 | 47.14 | 28655 |
| Skywrath Mage | -0.88 | 45.01 | 55347 |
| Dark Willow | -0.89 | 46.54 | 32868 |
| Invoker | -0.92 | 44.87 | 89378 |
| Tidehunter | -0.99 | 45.43 | 32947 |
| Omniknight | -1.01 | 45.02 | 10525 |
| Phantom Assassin | -1.04 | 43.99 | 61905 |
| Nyx Assassin | -1.04 | 42.89 | 27333 |
| Leshrac | -1.06 | 44.84 | 9037 |
| Lion | -1.15 | 46.24 | 120034 |
| Ursa | -1.15 | 47.87 | 27426 |
| Bloodseeker | -1.21 | 44.08 | 18016 |
| Arc Warden | -1.30 | 43.18 | 17960 |
| Earthshaker | -1.35 | 45.09 | 69927 |
| Legion Commander | -1.47 | 42.89 | 64336 |
| Pugna | -1.65 | 45.65 | 19183 |
| Primal Beast | -1.72 | 46.70 | 11517 |
| Anti-Mage | -1.74 | 45.91 | 46806 |
| Beastmaster | -1.74 | 51.41 | 9920 |
| Morphling | -1.75 | 48.46 | 15841 |
| Elder Titan | -1.83 | 45.39 | 5486 |
| Abaddon | -2.04 | 45.24 | 14276 |
| Axe | -2.08 | 45.73 | 86153 |
| Broodmother | -2.14 | 46.20 | 8561 |
| Riki | -2.19 | 44.06 | 23568 |
| Drow Ranger | -2.20 | 50.01 | 53683 |
| Spirit Breaker | -2.50 | 44.65 | 76755 |
| Meepo | -2.52 | 44.36 | 7817 |
| Naga Siren | -2.73 | 48.08 | 7460 |
| Puck | -2.92 | 50.69 | 14964 |
| Slark | -3.03 | 46.97 | 50204 |
| Phantom Lancer | -3.34 | 44.41 | 51550 |
