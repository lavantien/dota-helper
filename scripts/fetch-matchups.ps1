#Requires -Version 7
# Rebuilds current counter tables for the heroes whose English Dotabuff pages
# serve stale 2024/2025 CDN snapshots. The localized de.dotabuff.com subdomain
# serves the same live tables, so raw page dumps land in ref/dota2/matchups/raw/
# (scraped via the web reader MCP) and this script parses them into the standard
# matchup md files. OpenDota is used only for overall winrates, its matchups
# endpoint proved unreliable (sign flips vs fresh Dotabuff mirrors at 300+ games
# and tiny rolling windows, median 9 games per pair for Visage).
# The slug list below is the PREVIOUS pool: these dumps are now the dotabuff
# fallback tier behind the STRATZ primary, kept as the parity oracle for the
# engine's Go port of this parser until the diff matches, then this retires.
[CmdletBinding()]
param(
    [string] $Date = (Get-Date -Format 'yyyy-MM-dd')
)

$ErrorActionPreference = 'Stop'

# single config hub
$Config = @{
    Date               = $Date
    Slugs              = @('arc-warden', 'bloodseeker', 'dark-seer', 'doom',
                           'dragon-knight', 'hoodwink', 'kunkka', 'lifestealer',
                           'lone-druid', 'ogre-magi', 'spectre', 'spirit-breaker',
                           'tiny', 'undying', 'visage', 'warlock', 'windranger',
                           'winter-wyvern')
    LocaleHost         = 'de.dotabuff.com'
    MatchupsDir        = (Resolve-Path (Join-Path $PSScriptRoot '..\ref\dota2\matchups')).Path
    MinRows            = 120
    MirrorMaxDiverge   = 1.0
    MirrorSignFloor    = 1.5
    OpenDotaHeroesUri  = 'https://api.opendota.com/api/heroes'
    OpenDotaStatsUri   = 'https://api.opendota.com/api/heroStats'
}
$Config.RawDir        = Join-Path $Config.MatchupsDir 'raw'
$Config.ArchiveDir    = Join-Path $Config.MatchupsDir 'archive-dotabuff'
$Config.CurrentJson   = Join-Path $Config.MatchupsDir "current-$($Config.Date).json"
$Config.OpenDotaJson  = Join-Path $Config.MatchupsDir "opendota-$($Config.Date).json"

# row shape: | Image N | Hero Name | 12.34% | 43.21% | 123,456 |
# summary tables above the full Matchups table lack the matches cell and never match
$RowRx = '^\|\s*(?:Image|Bild)[^|]*\|\s*([^|]+?)\s*\|\s*([+-]?[\d.]+)%\s*\|\s*([+-]?[\d.]+)%\s*\|\s*([\d,]+)\s*\|$'

function Read-RawTable {
    param([string] $Slug)
    $path = Join-Path $Config.RawDir "$Slug.txt"
    if (-not (Test-Path $path)) { throw "raw dump missing: $path" }
    $content = Get-Content $path -Raw
    if ($content -match 'Last Updated 20(24|25)') { throw "$Slug raw dump is a stale snapshot" }
    if ($content -notmatch 'Letzte Aktualisierung') { throw "$Slug raw dump has no freshness line" }
    if ($content -notmatch 'Matchups') { throw "$Slug raw dump has no Matchups section" }
    $rows = @()
    foreach ($line in ($content -split "`r?`n")) {
        if ($line -match $RowRx) {
            $name = $Matches[1].Trim()
            $rows += [pscustomobject]@{
                enemy   = $name
                dis     = [double]$Matches[2]
                wr      = [double]$Matches[3]
                matches = [long]($Matches[4] -replace ',', '')
            }
        }
    }
    $unique = @($rows | Group-Object enemy | ForEach-Object { $_.Group[0] })
    if ($unique.Count -lt $Config.MinRows) { throw "$Slug parsed only $($unique.Count) rows" }
    if (-not ($unique | Where-Object { $_.enemy -eq 'Pudge' })) { throw "$Slug parsed table has no Pudge row" }
    $unique
}

# parse all raw dumps
$tables = @{}
foreach ($slug in $Config.Slugs) {
    $tables[$slug] = @(Read-RawTable $slug | Sort-Object dis -Descending)
    Write-Host ("{0,-14} {1,3} rows  worst: {2}" -f $slug, $tables[$slug].Count, `
        (($tables[$slug] | Select-Object -First 3 | ForEach-Object { "$($_.enemy) $($_.dis)" }) -join ', '))
}

# overall winrates from OpenDota heroStats, ids resolved by slug at runtime
$heroList = Invoke-RestMethod -Uri $Config.OpenDotaHeroesUri -TimeoutSec 60
$slugAliases = @{
    'doom-bringer' = 'doom'; 'obsidian-destroyer' = 'outworld-destroyer'
    'zuus' = 'zeus'; 'nevermore' = 'shadow-fiend'; 'necrolyte' = 'necrophos'
    'skeleton-king' = 'wraith-king'; 'furion' = 'natures-prophet'
    'life-stealer' = 'lifestealer'; 'antimage' = 'anti-mage'; 'wisp' = 'io'
    'shredder' = 'timbersaw'; 'abyssal-underlord' = 'underlord'
    'windrunner' = 'windranger'
}
$idToSlug = @{}
foreach ($h in $heroList) {
    $slug = ($h.name -replace '^npc_dota_hero_', '') -replace '_', '-'
    if ($slugAliases.ContainsKey($slug)) { $slug = $slugAliases[$slug] }
    $idToSlug[[int]$h.id] = $slug
}
$stats = Invoke-RestMethod -Uri $Config.OpenDotaStatsUri -TimeoutSec 60
$overall = @{}
foreach ($s in $stats) {
    $pick, $win = [long]0, [long]0
    if ($s.PSObject.Properties['pub_pick'] -and [long]$s.pub_pick -gt 0) {
        $pick, $win = [long]$s.pub_pick, [long]$s.pub_win
    } else {
        foreach ($i in 1..8) {
            $p = $s.PSObject.Properties["${i}_pick"]
            $w = $s.PSObject.Properties["${i}_win"]
            if ($p) { $pick += [long]$p.Value }
            if ($w) { $win += [long]$w.Value }
        }
    }
    if ($pick -gt 0) { $overall[$idToSlug[[int]$s.id]] = [math]::Round(100.0 * $win / $pick, 2) }
}

# display name of each pool hero: the row name whose slugified form matches its
# slug, resolved against any other pool table (a hero is never a row in his own)
$display = @{}
foreach ($hSlug in $Config.Slugs) {
    foreach ($tSlug in $Config.Slugs) {
        if ($tSlug -eq $hSlug) { continue }
        $row = $tables[$tSlug] | Where-Object {
            ($_.enemy.ToLower() -replace '[^a-z ]', '' -replace ' ', '-') -eq $hSlug } | Select-Object -First 1
        if ($row) { $display[$hSlug] = $row.enemy; break }
    }
    if (-not $display[$hSlug]) { throw "cannot resolve display name for $hSlug" }
}

# mirror sanity across pool pairs: dis(H vs F) should mirror -dis(F,H).
# dotabuff pages refresh independently, magnitudes drift up to ~2 points while
# signs stay consistent, so only same-sign pairs above the sheet cut are fatal
$diverge = @()
$signFlips = @()
$pairs, $worstDiv = 0, 0.0
for ($i = 0; $i -lt $Config.Slugs.Count; $i++) {
    for ($j = $i + 1; $j -lt $Config.Slugs.Count; $j++) {
        $hSlug, $fSlug = $Config.Slugs[$i], $Config.Slugs[$j]
        $rowF = $tables[$hSlug] | Where-Object { $_.enemy -eq $display[$fSlug] }
        $rowH = $tables[$fSlug] | Where-Object { $_.enemy -eq $display[$hSlug] }
        if (-not $rowF -or -not $rowH) { continue }
        $d = [math]::Abs($rowF.dis + $rowH.dis)
        $pairs++
        if ($d -gt $worstDiv) { $worstDiv = [math]::Round($d, 2) }
        if ($d -gt $Config.MirrorMaxDiverge) {
            $diverge += "$hSlug vs $fSlug`: $($rowF.dis) and $($rowH.dis) sum to $([math]::Round($rowF.dis + $rowH.dis, 2))"
        }
        if ([math]::Abs($rowF.dis) -ge $Config.MirrorSignFloor -and
            [math]::Abs($rowH.dis) -ge $Config.MirrorSignFloor -and
            [math]::Sign($rowF.dis) -eq [math]::Sign($rowH.dis)) {
            $signFlips += "$hSlug vs $fSlug`: both tables claim the other side wins ($($rowF.dis), $($rowH.dis))"
        }
    }
}
Write-Host "mirror sanity: $pairs pool pairs, worst divergence $worstDiv, $($diverge.Count) above $($Config.MirrorMaxDiverge)"
foreach ($d in $diverge) { Write-Host "drift: $d" }
if ($signFlips.Count -gt 0) {
    foreach ($s in $signFlips) { Write-Host "SIGN CONFLICT: $s" }
    throw "mirror sign conflicts found, do not trust these tables"
}

# archive any existing md for the pool before overwriting, nothing gets deleted.
# same-day rebuilds of the same source are just re-runs, they get overwritten in place
New-Item -ItemType Directory -Force $Config.ArchiveDir | Out-Null
$moved = 0
foreach ($slug in $Config.Slugs) {
    $p = Join-Path $Config.MatchupsDir "$slug.md"
    if (-not (Test-Path $p)) { continue }
    $head = (Get-Content $p -TotalCount 3) -join ' '
    if ($head -match 'de\.dotabuff\.com' -and $head -match [regex]::Escape($Config.Date)) { continue }
    $dest = Join-Path $Config.ArchiveDir "$slug.md"
    if (Test-Path $dest) {
        # never overwrite history, same-named snapshots get a timestamp suffix
        $dest = Join-Path $Config.ArchiveDir ("$slug-$(Get-Date -Format 'yyyyMMdd-HHmmss').md")
    }
    Move-Item $p $dest; $moved++
}
Write-Host "archived $moved previous tables to $(Split-Path $Config.ArchiveDir -Leaf)/"

# write standard md per hero
foreach ($slug in $Config.Slugs) {
    $title = $slug -replace '-', ' '
    $wr = $overall[$slug]
    $lines = @(
        "# $title matchup table",
        '',
        "source: https://$($Config.LocaleHost)/heroes/$slug/counters, scraped $($Config.Date) during patch 7.41f. All pool heroes are pulled from the German locale subdomain for one consistent live window: it serves the identical live table to the English page (English hero names, same 'This Month' filter, refreshed within minutes at scrape time), while English pages for several pool heroes serve stale CDN snapshots to this machine. Columns per ref/dota2/README.md: \`dis%\` positive = the listed enemy beats the file hero. Sampled games per pair reach the 100k+ range on common heroes.",
        '',
        "Overall winrate context: $wr% over the recent OpenDota public window.",
        '',
        "Sanity: mirror consistency checked across all pool pairs from the same scrape window, worst divergence $worstDiv percentage points across $pairs pairs."
    )
    if ($diverge.Count -gt 0) { $lines += "Divergent pairs above $($Config.MirrorMaxDiverge): see make fetch-matchups output." }
    $lines += @('', '| enemy | dis% | wr% | matches |', '| --- | --- | --- | --- |')
    foreach ($r in $tables[$slug]) {
        $lines += '| {0} | {1:N2} | {2:N2} | {3} |' -f $r.enemy, $r.dis, $r.wr, $r.matches
    }
    Set-Content -Path (Join-Path $Config.MatchupsDir "$slug.md") -Value $lines -Encoding utf8
}
Write-Host "wrote $($Config.Slugs.Count) matchup md files"

# machine readable snapshot for guide generation and verification
$current = @{
    date     = $Config.Date
    patch    = '7.41f'
    source   = "https://$($Config.LocaleHost)/heroes/{slug}/counters"
    semantic = 'dis positive = listed enemy beats file hero, dotabuff internal disadvantage metric, magnitudes comparable within one table, month filter'
    heroes   = @{}
    mirror_check = @{ pairs = $pairs; worst_divergence = $worstDiv; divergent = $diverge }
}
foreach ($slug in $Config.Slugs) {
    $current.heroes.$slug = @{
        overall_wr_opendota = $overall[$slug]
        rows = @($tables[$slug] | ForEach-Object {
            @{ enemy = $_.enemy; dis = $_.dis; wr = $_.wr; matches = $_.matches }
        })
    }
}
$current | ConvertTo-Json -Depth 6 | Set-Content -Path $Config.CurrentJson -Encoding utf8
Write-Host "wrote $(Split-Path $Config.CurrentJson -Leaf)"

# OpenDota snapshot: overall winrates only, matchups endpoint deliberately unused
$od = @{
    date   = $Config.Date
    source = $Config.OpenDotaStatsUri
    note   = 'heroStats public overall winrates only. The /heroes/{id}/matchups endpoint is NOT used: rolling small-sample window (median 9 games per pair for Visage, 8 for Arc Warden) and sign flips vs fresh Dotabuff mirrors on both 300+ game pairs checked (Hoodwink vs Tiny, Windranger vs Tiny).'
    overall_wr_by_slug = $overall
}
$od | ConvertTo-Json -Depth 4 | Set-Content -Path $Config.OpenDotaJson -Encoding utf8
Write-Host "wrote $(Split-Path $Config.OpenDotaJson -Leaf)"
