# doom matchup table

source: https://de.dotabuff.com/heroes/doom/counters, scraped 2026-09-24 during patch 7.41f. All pool heroes are pulled from the German locale subdomain for one consistent live window: it serves the identical live table to the English page (English hero names, same 'This Month' filter, refreshed within minutes at scrape time), while English pages for several pool heroes serve stale CDN snapshots to this machine. Columns per ref/dota2/README.md: \dis%\ positive = the listed enemy beats the file hero. Sampled games per pair reach the 100k+ range on common heroes.

Overall winrate context: 47.83% over the recent OpenDota public window.

Sanity: mirror consistency checked across all pool pairs from the same scrape window, worst divergence 1.81 percentage points across 153 pairs.
Divergent pairs above 1: see make fetch-matchups output.

| enemy | dis% | wr% | matches |
| --- | --- | --- | --- |
| Medusa | 3.64 | 44.04 | 23170 |
| Arc Warden | 3.57 | 41.28 | 22931 |
| Lone Druid | 3.15 | 46.28 | 24605 |
| Broodmother | 2.98 | 44.03 | 9618 |
| Nature's Prophet | 2.38 | 52.28 | 56576 |
| Terrorblade | 2.31 | 48.33 | 30344 |
| Clinkz | 2.30 | 45.28 | 26905 |
| Lina | 2.09 | 46.26 | 120626 |
| Lycan | 2.02 | 47.16 | 7000 |
| Wraith King | 1.90 | 40.76 | 81084 |
| Marci | 1.81 | 45.80 | 20884 |
| Naga Siren | 1.69 | 46.64 | 8529 |
| Shadow Fiend | 1.48 | 48.64 | 121508 |
| Beastmaster | 1.37 | 51.36 | 11170 |
| Chaos Knight | 1.36 | 44.43 | 24317 |
| Meepo | 1.31 | 43.49 | 9031 |
| Keeper of the Light | 1.30 | 46.34 | 34217 |
| Spectre | 1.26 | 42.30 | 68442 |
| Enigma | 1.25 | 45.03 | 23108 |
| Invoker | 1.22 | 45.92 | 112857 |
| Drow Ranger | 1.17 | 49.85 | 65279 |
| Tusk | 1.10 | 48.23 | 33935 |
| Enchantress | 1.08 | 48.34 | 16282 |
| Luna | 1.04 | 45.86 | 56894 |
| Viper | 1.03 | 48.05 | 40928 |
| Night Stalker | 1.02 | 44.58 | 46984 |
| Vengeful Spirit | 0.94 | 43.48 | 65931 |
| Weaver | 0.90 | 49.23 | 27408 |
| Brewmaster | 0.89 | 44.99 | 9414 |
| Crystal Maiden | 0.82 | 44.91 | 75852 |
| Sniper | 0.78 | 46.59 | 108788 |
| Leshrac | 0.77 | 46.02 | 11270 |
| Windranger | 0.76 | 48.02 | 96744 |
| Venomancer | 0.75 | 49.12 | 37589 |
| Phantom Lancer | 0.71 | 43.48 | 70900 |
| Witch Doctor | 0.68 | 44.65 | 79923 |
| Grimstroke | 0.66 | 44.88 | 32956 |
| Jakiro | 0.59 | 48.19 | 45222 |
| Bane | 0.49 | 47.21 | 21498 |
| Undying | 0.44 | 46.54 | 85115 |
| Tinker | 0.43 | 49.92 | 27336 |
| Skywrath Mage | 0.41 | 46.82 | 65901 |
| Riki | 0.38 | 44.51 | 28562 |
| Spirit Breaker | 0.37 | 44.97 | 87771 |
| Bristleback | 0.34 | 47.94 | 36223 |
| Magnus | 0.27 | 48.00 | 64985 |
| Death Prophet | 0.27 | 48.60 | 21464 |
| Kez | 0.22 | 52.23 | 26906 |
| Legion Commander | 0.21 | 44.30 | 77766 |
| Razor | 0.20 | 46.94 | 37737 |
| Rubick | 0.16 | 47.50 | 113313 |
| Ogre Magi | 0.16 | 46.55 | 93786 |
| Visage | 0.16 | 44.45 | 8176 |
| Bounty Hunter | 0.09 | 43.76 | 54208 |
| Batrider | 0.09 | 53.33 | 5352 |
| Templar Assassin | 0.05 | 51.64 | 28862 |
| Io | 0.05 | 48.92 | 29207 |
| Shadow Demon | 0.04 | 52.43 | 12964 |
| Zeus | 0.02 | 47.45 | 77420 |
| Anti-Mage | -0.01 | 47.27 | 63936 |
| Techies | -0.05 | 46.65 | 58426 |
| Centaur Warrunner | -0.05 | 47.18 | 55279 |
| Shadow Shaman | -0.06 | 45.50 | 77918 |
| Sand King | -0.07 | 48.26 | 23687 |
| Tiny | -0.10 | 53.22 | 28919 |
| Silencer | -0.12 | 46.96 | 58317 |
| Elder Titan | -0.15 | 46.72 | 6265 |
| Dark Willow | -0.21 | 48.92 | 42715 |
| Hoodwink | -0.24 | 50.12 | 74215 |
| Ancient Apparition | -0.24 | 45.90 | 37542 |
| Lich | -0.26 | 44.90 | 59362 |
| Bloodseeker | -0.31 | 46.24 | 17290 |
| Pudge | -0.32 | 46.34 | 167415 |
| Puck | -0.32 | 51.17 | 19251 |
| Storm Spirit | -0.32 | 51.27 | 37650 |
| Clockwerk | -0.34 | 49.70 | 25568 |
| Gyrocopter | -0.37 | 52.33 | 14807 |
| Phantom Assassin | -0.44 | 46.45 | 67128 |
| Pangolier | -0.44 | 51.57 | 23110 |
| Warlock | -0.45 | 48.38 | 29735 |
| Mirana | -0.45 | 46.06 | 84826 |
| Lion | -0.54 | 48.77 | 136921 |
| Oracle | -0.61 | 47.62 | 21216 |
| Disruptor | -0.61 | 47.80 | 47695 |
| Monkey King | -0.62 | 52.53 | 24702 |
| Earthshaker | -0.63 | 47.44 | 85621 |
| Dazzle | -0.64 | 46.99 | 23418 |
| Snapfire | -0.65 | 48.90 | 64654 |
| Chen | -0.66 | 53.21 | 2569 |
| Treant Protector | -0.68 | 49.31 | 29270 |
| Muerta | -0.74 | 50.31 | 14909 |
| Slardar | -0.79 | 47.97 | 50959 |
| Primal Beast | -0.82 | 48.85 | 14761 |
| Ringmaster | -0.83 | 49.73 | 29198 |
| Pugna | -0.83 | 47.87 | 23384 |
| Mars | -0.86 | 51.04 | 24356 |
| Dragon Knight | -0.87 | 46.74 | 39456 |
| Dark Seer | -0.89 | 49.07 | 35097 |
| Kunkka | -0.91 | 48.85 | 23844 |
| Queen of Pain | -0.93 | 51.02 | 54862 |
| Ursa | -0.94 | 50.75 | 27144 |
| Phoenix | -1.02 | 46.80 | 30914 |
| Tidehunter | -1.03 | 48.52 | 43383 |
| Troll Warlord | -1.05 | 46.70 | 13508 |
| Outworld Destroyer | -1.11 | 45.25 | 50025 |
| Earth Spirit | -1.12 | 48.92 | 45687 |
| Axe | -1.20 | 47.96 | 110549 |
| Winter Wyvern | -1.24 | 48.58 | 44387 |
| Alchemist | -1.27 | 51.81 | 17168 |
| Void Spirit | -1.27 | 48.95 | 23775 |
| Juggernaut | -1.43 | 46.48 | 101489 |
| Omniknight | -1.54 | 48.60 | 13232 |
| Huskar | -1.63 | 53.75 | 20885 |
| Sven | -1.69 | 48.08 | 49724 |
| Abaddon | -1.77 | 48.01 | 18556 |
| Nyx Assassin | -1.80 | 46.67 | 34902 |
| Underlord | -1.92 | 49.46 | 77869 |
| Dawnbreaker | -1.97 | 47.25 | 53256 |
| Largo | -2.13 | 51.79 | 12322 |
| Ember Spirit | -2.32 | 50.46 | 51591 |
| Slark | -2.32 | 49.34 | 60755 |
| Morphling | -2.43 | 52.22 | 18482 |
| Timbersaw | -2.78 | 55.04 | 28846 |
| Faceless Void | -2.89 | 51.46 | 47463 |
| Necrophos | -3.38 | 49.28 | 99530 |
| Lifestealer | -3.55 | 47.99 | 98694 |
