# warlock matchup table

source: https://de.dotabuff.com/heroes/warlock/counters, scraped 2026-09-24 during patch 7.41f. All pool heroes are pulled from the German locale subdomain for one consistent live window: it serves the identical live table to the English page (English hero names, same 'This Month' filter, refreshed within minutes at scrape time), while English pages for several pool heroes serve stale CDN snapshots to this machine. Columns per ref/dota2/README.md: \dis%\ positive = the listed enemy beats the file hero. Sampled games per pair reach the 100k+ range on common heroes.

Overall winrate context: 49.81% over the recent OpenDota public window.

Sanity: mirror consistency checked across all pool pairs from the same scrape window, worst divergence 1.81 percentage points across 153 pairs.
Divergent pairs above 1: see make fetch-matchups output.

| enemy | dis% | wr% | matches |
| --- | --- | --- | --- |
| Largo | 2.36 | 49.26 | 9536 |
| Slark | 2.26 | 46.95 | 57256 |
| Morphling | 1.97 | 49.85 | 19934 |
| Underlord | 1.92 | 47.83 | 60265 |
| Tidehunter | 1.87 | 47.72 | 40246 |
| Phantom Assassin | 1.85 | 46.34 | 83432 |
| Sand King | 1.76 | 48.49 | 23121 |
| Bounty Hunter | 1.76 | 44.22 | 46192 |
| Phoenix | 1.70 | 46.17 | 26783 |
| Weaver | 1.64 | 50.57 | 29764 |
| Shadow Fiend | 1.60 | 50.59 | 116709 |
| Monkey King | 1.59 | 52.37 | 24203 |
| Bristleback | 1.58 | 48.77 | 41246 |
| Sven | 1.56 | 46.96 | 48649 |
| Faceless Void | 1.38 | 49.32 | 46987 |
| Kez | 1.28 | 53.22 | 24501 |
| Pangolier | 1.27 | 51.88 | 18002 |
| Slardar | 1.18 | 48.11 | 47137 |
| Sniper | 1.13 | 48.34 | 119113 |
| Leshrac | 1.08 | 47.79 | 10181 |
| Elder Titan | 0.96 | 47.67 | 5974 |
| Snapfire | 0.92 | 49.44 | 57350 |
| Legion Commander | 0.92 | 45.70 | 80893 |
| Troll Warlord | 0.89 | 46.78 | 15933 |
| Earth Spirit | 0.86 | 49.04 | 33626 |
| Jakiro | 0.72 | 50.14 | 49641 |
| Tusk | 0.66 | 50.74 | 28827 |
| Ursa | 0.66 | 51.21 | 30416 |
| Clinkz | 0.64 | 49.02 | 27068 |
| Queen of Pain | 0.61 | 51.58 | 49001 |
| Earthshaker | 0.59 | 48.36 | 80823 |
| Batrider | 0.58 | 54.87 | 4842 |
| Windranger | 0.58 | 50.26 | 90046 |
| Timbersaw | 0.55 | 53.74 | 23695 |
| Anti-Mage | 0.53 | 48.82 | 59795 |
| Primal Beast | 0.51 | 49.58 | 13039 |
| Brewmaster | 0.51 | 47.49 | 7615 |
| Razor | 0.50 | 48.72 | 33576 |
| Lone Druid | 0.46 | 51.07 | 20104 |
| Alchemist | 0.45 | 52.13 | 15888 |
| Doom | 0.44 | 51.62 | 29734 |
| Crystal Maiden | 0.36 | 47.44 | 80449 |
| Ancient Apparition | 0.32 | 47.43 | 38408 |
| Muerta | 0.32 | 51.31 | 15536 |
| Ember Spirit | 0.31 | 49.95 | 42647 |
| Storm Spirit | 0.29 | 52.72 | 34273 |
| Lich | 0.23 | 46.51 | 60409 |
| Keeper of the Light | 0.23 | 49.47 | 31756 |
| Invoker | 0.22 | 48.92 | 102352 |
| Spirit Breaker | 0.18 | 47.23 | 83427 |
| Undying | 0.14 | 48.90 | 82456 |
| Chen | 0.13 | 54.46 | 2343 |
| Void Spirit | 0.13 | 49.62 | 20360 |
| Night Stalker | 0.12 | 47.57 | 39605 |
| Lifestealer | 0.08 | 46.66 | 94918 |
| Mars | 0.08 | 52.15 | 20744 |
| Bloodseeker | 0.06 | 47.92 | 20013 |
| Kunkka | 0.06 | 49.95 | 23043 |
| Dark Willow | 0.05 | 50.75 | 36399 |
| Techies | 0.04 | 48.64 | 60789 |
| Abaddon | 0.04 | 48.26 | 17475 |
| Mirana | 0.03 | 47.70 | 73479 |
| Dawnbreaker | 0.02 | 47.38 | 47463 |
| Marci | -0.01 | 49.71 | 19486 |
| Drow Ranger | -0.01 | 53.05 | 67687 |
| Oracle | -0.02 | 49.10 | 18663 |
| Nature's Prophet | -0.03 | 56.69 | 55352 |
| Luna | -0.05 | 49.01 | 54221 |
| Io | -0.07 | 51.12 | 22756 |
| Nyx Assassin | -0.08 | 47.05 | 28897 |
| Magnus | -0.10 | 50.44 | 56627 |
| Viper | -0.10 | 51.24 | 43612 |
| Necrophos | -0.10 | 48.27 | 88836 |
| Juggernaut | -0.10 | 47.30 | 97166 |
| Lina | -0.11 | 50.35 | 115033 |
| Gyrocopter | -0.12 | 54.15 | 14363 |
| Huskar | -0.12 | 54.29 | 21108 |
| Treant Protector | -0.14 | 50.84 | 26838 |
| Death Prophet | -0.16 | 51.11 | 19995 |
| Medusa | -0.17 | 50.05 | 16109 |
| Riki | -0.18 | 47.15 | 28555 |
| Templar Assassin | -0.20 | 53.96 | 28022 |
| Enchantress | -0.21 | 51.73 | 15035 |
| Ringmaster | -0.26 | 51.22 | 25726 |
| Hoodwink | -0.26 | 52.20 | 69895 |
| Venomancer | -0.30 | 52.24 | 39782 |
| Pudge | -0.31 | 48.41 | 167735 |
| Rubick | -0.34 | 50.03 | 113092 |
| Disruptor | -0.35 | 49.62 | 43308 |
| Outworld Destroyer | -0.36 | 46.61 | 42780 |
| Tiny | -0.36 | 55.53 | 24076 |
| Lion | -0.37 | 50.67 | 144318 |
| Silencer | -0.39 | 49.29 | 57900 |
| Puck | -0.40 | 53.30 | 16464 |
| Zeus | -0.43 | 49.96 | 73494 |
| Shadow Demon | -0.44 | 54.98 | 11798 |
| Dazzle | -0.45 | 48.86 | 25774 |
| Pugna | -0.52 | 49.64 | 22521 |
| Dragon Knight | -0.67 | 48.63 | 35609 |
| Skywrath Mage | -0.70 | 49.97 | 66158 |
| Vengeful Spirit | -0.71 | 47.17 | 60527 |
| Lycan | -0.72 | 52.04 | 6453 |
| Dark Seer | -0.77 | 51.04 | 27894 |
| Bane | -0.80 | 50.60 | 19598 |
| Witch Doctor | -0.88 | 48.22 | 92551 |
| Centaur Warrunner | -1.04 | 50.25 | 44240 |
| Clockwerk | -1.05 | 52.49 | 21294 |
| Axe | -1.18 | 50.02 | 96115 |
| Winter Wyvern | -1.19 | 50.63 | 37202 |
| Broodmother | -1.28 | 50.44 | 8816 |
| Shadow Shaman | -1.31 | 48.76 | 88814 |
| Ogre Magi | -1.39 | 50.07 | 96479 |
| Tinker | -1.41 | 53.83 | 23912 |
| Grimstroke | -1.48 | 49.10 | 32544 |
| Arc Warden | -1.70 | 48.68 | 22007 |
| Spectre | -2.03 | 47.57 | 63279 |
| Visage | -2.14 | 48.89 | 7236 |
| Omniknight | -2.36 | 51.52 | 12390 |
| Naga Siren | -2.55 | 53.05 | 7778 |
| Beastmaster | -2.57 | 57.42 | 10371 |
| Meepo | -2.57 | 49.54 | 8153 |
| Enigma | -2.82 | 51.22 | 19937 |
| Chaos Knight | -3.03 | 50.92 | 25523 |
| Terrorblade | -3.72 | 56.43 | 25526 |
| Phantom Lancer | -4.59 | 50.71 | 59117 |
| Wraith King | -4.75 | 49.28 | 65272 |
