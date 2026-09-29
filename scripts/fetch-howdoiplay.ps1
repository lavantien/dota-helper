#Requires -Version 7
# Crawls Tsunami's howdoiplay.com tip pages for every pool hero and writes the
# ref/dota2/howdoiplay/ snapshot: verbatim page dumps under raw/ plus the
# structured tips/counters extraction in howdoiplay.json. Hero names resolve by
# exact match against the live HEROES roster embedded in the homepage, so name
# drift on the site fails loud instead of silently skipping a hero. Tip pages
# are patch-scoped static html: rerun via `make fetch-howdoiplay` when the
# patch series moves, there is no archive tier because pages are re-fetchable.
[CmdletBinding()]
param(
    [string] $Date = (Get-Date -Format 'yyyy-MM-dd')
)

$ErrorActionPreference = 'Stop'

# single config hub; endpoints.howdoiplay comes from config.json
$Config = @{
    Date              = $Date
    Root              = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
    UserAgent         = 'howdoiplay-crawl/1.0 (personal pick simulator ref snapshot)'
    MinRosterNames    = 120   # live HEROES roster is 127, guard against parsing a redesigned page
    MinTips           = 3     # structural sanity floors, short pages are real (dragon knight has 4 tips)
    MinCounters       = 2
    WordCoverage      = 0.90  # aggregate backstop only: bounds total loss to 10%. the parser's
                              # no-stray-text throw is the per-entry text-loss guard
    RequestIntervalMs = 250
    TimeoutSec        = 60
    FetchAttempts     = 3     # cold pages sometimes serve for tens of seconds, retry before failing the crawl
}
$Config.CfgPath = Join-Path $Config.Root 'config.json'
$Config.Dir     = Join-Path $Config.Root 'ref/dota2/howdoiplay'
$Config.RawDir  = Join-Path $Config.Dir 'raw'

function Get-HttpBytes {
    param([string] $Uri)
    for ($attempt = 1; $attempt -le $Config.FetchAttempts; $attempt++) {
        $tmp = New-TemporaryFile
        try {
            $resp = Invoke-WebRequest -Uri $Uri -UserAgent $Config.UserAgent `
                -SkipHttpErrorCheck -TimeoutSec $Config.TimeoutSec -OutFile $tmp -PassThru
            return [pscustomobject]@{ Status = [int]$resp.StatusCode; Bytes = [IO.File]::ReadAllBytes($tmp) }
        } catch {
            if ($attempt -ge $Config.FetchAttempts) { throw }
            Start-Sleep -Seconds (2 * $attempt)
        } finally {
            Remove-Item $tmp -ErrorAction SilentlyContinue
        }
    }
    throw "unreachable: fetch attempts exhausted for $Uri"
}

# strip leftover inline tags, decode entities, collapse whitespace
function ConvertTo-CleanText {
    param([string] $Raw)
    if (-not $Raw) { return '' }
    $t = $Raw -replace '<[^>]+>', ' '
    $t = [System.Net.WebUtility]::HtmlDecode($t)
    ($t -replace '\s+', ' ').Trim()
}

# a section is the html between its <h1> and the next h1/end: <ul> lists whose
# <li> are entries, and <ul> lists opened inside an open li whose <li> are notes
# elaborating the parent entry. the html is loose (li frequently unclosed around
# the nested list, and the site sometimes emits a sibling <ul> after a closed
# li, whose items are top-level entries), so this walks tag tokens with a
# list-context stack instead of trusting an xml parser. text outside any li is
# collected and fails the parse loudly: no silent drops.
function ConvertTo-SectionEntries {
    param([string] $SectionHtml)
    $entries = @()
    $entryText = ''
    $noteText = ''
    $notes = @()
    $stray = @()
    # parallel stacks per open <ul>: its kind ('entries' top/sibling list,
    # 'notes' list inside an open li) and whether one of its li is open
    $lists = @()
    $liOpen = @()
    $pos = 0

    foreach ($m in [regex]::Matches($SectionHtml, '<(/?)(ul|li)(?:\s[^>]*)?>')) {
        $clean = ConvertTo-CleanText $SectionHtml.Substring($pos, $m.Index - $pos)
        $pos = $m.Index + $m.Length
        # text goes to the innermost open li: a note in a notes list, the entry
        # in an entries list, stray anywhere else (fails the parse at the end)
        if ($clean) {
            if ($lists.Count -eq 0 -or -not $liOpen[-1]) { $stray += $clean }
            elseif ($lists[-1] -eq 'notes') { $noteText = ($noteText + ' ' + $clean).Trim() }
            else { $entryText = ($entryText + ' ' + $clean).Trim() }
        }
        $closing, $tag = $m.Groups[1].Value, $m.Groups[2].Value
        if ($tag -eq 'ul') {
            if ($closing) {
                if ($lists.Count -eq 0) { continue }
                if ($liOpen[-1]) {
                    if ($lists[-1] -eq 'notes') {
                        if ($noteText) { $notes += $noteText }; $noteText = ''
                    } else {
                        if ($entryText -or $notes.Count) { $entries += [pscustomobject]@{ text = $entryText; notes = $notes } }
                        $entryText = ''; $notes = @()
                    }
                    $liOpen[-1] = $false
                }
                if ($lists.Count -le 1) { $lists = @(); $liOpen = @() }
                else { $lists = $lists[0 .. ($lists.Count - 2)]; $liOpen = $liOpen[0 .. ($liOpen.Count - 2)] }
            } else {
                # a list opened inside an open li elaborates it (notes); any
                # other list is a sibling of the entry list, its items are entries
                $kind = if ($lists.Count -gt 0 -and $liOpen[-1]) { 'notes' } else { 'entries' }
                $lists += $kind
                $liOpen += $false
            }
        } else {
            if ($lists.Count -eq 0) { continue }
            if ($closing) {
                if ($liOpen[-1]) {
                    if ($lists[-1] -eq 'notes') {
                        if ($noteText) { $notes += $noteText }; $noteText = ''
                    } else {
                        if ($entryText -or $notes.Count) { $entries += [pscustomobject]@{ text = $entryText; notes = $notes } }
                        $entryText = ''; $notes = @()
                    }
                    $liOpen[-1] = $false
                }
            } else {
                # html lets the next li implicitly close the open one
                if ($liOpen[-1] -and $lists[-1] -eq 'notes') {
                    if ($noteText) { $notes += $noteText }; $noteText = ''
                } elseif ($liOpen[-1]) {
                    if ($entryText -or $notes.Count) { $entries += [pscustomobject]@{ text = $entryText; notes = $notes } }
                    $entryText = ''; $notes = @()
                }
                $liOpen[-1] = $true
            }
        }
    }
    $clean = ConvertTo-CleanText $SectionHtml.Substring($pos)
    if ($clean) {
        if ($lists.Count -eq 0 -or -not $liOpen[-1]) { $stray += $clean }
        elseif ($lists[-1] -eq 'notes') { $noteText = ($noteText + ' ' + $clean).Trim() }
        else { $entryText = ($entryText + ' ' + $clean).Trim() }
    }
    while ($lists.Count -gt 0) {
        if ($liOpen[-1]) {
            if ($lists[-1] -eq 'notes') {
                if ($noteText) { $notes += $noteText }; $noteText = ''
            } else {
                if ($entryText -or $notes.Count) { $entries += [pscustomobject]@{ text = $entryText; notes = $notes } }
                $entryText = ''; $notes = @()
            }
        }
        if ($lists.Count -le 1) { $lists = @(); $liOpen = @() }
        else { $lists = $lists[0 .. ($lists.Count - 2)]; $liOpen = $liOpen[0 .. ($liOpen.Count - 2)] }
    }
    if ($stray.Count -gt 0) {
        throw "text outside any li would be dropped: $($stray -join ' ')"
    }
    $entries
}

function Get-WordCount {
    param([string] $Text)
    @($Text -split '\s+' | Where-Object { $_ }).Count
}

# pool from the config hub, never hardcoded
$cfg = Get-Content $Config.CfgPath -Raw | ConvertFrom-Json
$base = $cfg.endpoints.howdoiplay
if (-not $base) { throw 'config.json endpoints.howdoiplay is missing' }
$pool = @($cfg.pool | Sort-Object slug -Unique)
if ($pool.Count -lt 2) { throw "config pool has $($pool.Count) heroes, refusing to crawl" }

# homepage carries the live roster and patch stamp
$homeResp = Get-HttpBytes "$base/"
$homeHtml = [Text.Encoding]::UTF8.GetString($homeResp.Bytes)
if ($homeResp.Status -ne 200) { throw "homepage returned $($homeResp.Status)" }
if ($homeHtml -notmatch "global\.PATCH\s*=\s*'([^']+)'") { throw 'homepage has no global.PATCH stamp' }
$patch = $Matches[1]
if ($homeHtml -notmatch '(?s)global\.HEROES\s*=\s*\[(.*?)\]') { throw 'homepage has no global.HEROES roster' }
$liveNames = @([regex]::Matches($Matches[1], "'((?:\\.|[^'\\])*)'") |
    ForEach-Object { $_.Groups[1].Value.Replace("\'", "'") })
if ($liveNames.Count -lt $Config.MinRosterNames) {
    throw "live HEROES roster parsed only $($liveNames.Count) names, below the $($Config.MinRosterNames) floor"
}

# exact name match, all misses reported at once
$misses = @($pool | Where-Object { $liveNames -cnotcontains $_.name } | ForEach-Object { $_.name })
if ($misses.Count -gt 0) {
    throw ("pool heroes missing from the live HEROES roster (site name drift?): " +
        ($misses -join ', ') + ". live roster: $($liveNames.Count) names, patch $patch")
}
Write-Host "roster: $($liveNames.Count) live names, patch $patch, all $($pool.Count) pool heroes resolve"

New-Item -ItemType Directory -Force $Config.RawDir | Out-Null
$crawlHeroes = @()
$derivedHeroes = [ordered]@{}
$now = { (Get-Date).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ') }

foreach ($h in $pool) {
    # slug rule mirrors the site's main.js: lowercase, spaces to underscores,
    # hyphens and apostrophes preserved (nature%27s_prophet)
    $tipSlug = $h.name.ToLowerInvariant() -replace ' ', '_'
    $url = "$base/tips/$([uri]::EscapeDataString($tipSlug)).html"
    $page = Get-HttpBytes $url
    $slug = $h.slug
    if ($page.Status -ne 200) { throw "$slug`: tip page returned $($page.Status)" }
    $html = [Text.Encoding]::UTF8.GetString($page.Bytes)
    if ($html -match 'What are you doing here\?') { throw "$slug`: tip page is the unknown-hero placeholder" }
    # identity: the name span, entity-decoded and whitespace-collapsed (some
    # pages use &nbsp;), must equal the pool hero name exactly
    $nameMatch = [regex]::Match($html, '<span class="name">([^<]*)</span>')
    if (-not $nameMatch.Success) { throw "$slug`: page has no name span" }
    $pageName = ConvertTo-CleanText $nameMatch.Groups[1].Value
    if ($pageName -cne $h.name) { throw "$slug`: page name span says '$pageName', pool says '$($h.name)'" }
    $tipsAt = $html.IndexOf('<h1>Tips</h1>')
    $countersAt = $html.IndexOf('<h1>Counters</h1>')
    if ($tipsAt -lt 0 -or $countersAt -lt 0 -or $countersAt -lt $tipsAt) {
        throw "$slug`: page lacks the Tips or Counters section"
    }
    # comments can embed li tags (invoker carries a commented-out bugfix li),
    # strip them before slicing so the tag walk and the coverage floor both
    # see only rendered content. kez's tips split into stance subsections whose
    # bold "<stance> Tips:" labels sit loose between li items, where they would
    # glue onto the previous entry or fail the no-stray-text guard: they are
    # presentation subheadings, dropped here with the same intent
    $tipsHtml = ($html.Substring($tipsAt + 13, $countersAt - $tipsAt - 13) -replace '(?s)<!--.*?-->', ' ') -replace '<b[^>]*>[^<]*</b>\s*Tips:', ' '
    $countersHtml = ($html.Substring($countersAt + 17) -replace '(?s)<!--.*?-->', ' ') -replace '<b[^>]*>[^<]*</b>\s*Tips:', ' '

    $fetchedAt = & $now
    [IO.File]::WriteAllBytes((Join-Path $Config.RawDir "$slug.html"), $page.Bytes)

    $tips = $null
    $counters = $null
    foreach ($pair in @(
        @{ Section = $tipsHtml; Name = 'tips'; Min = $Config.MinTips },
        @{ Section = $countersHtml; Name = 'counters'; Min = $Config.MinCounters }
    )) {
        $entries = @(ConvertTo-SectionEntries $pair.Section)
        if ($entries.Count -lt $pair.Min) {
            throw "$slug`: parsed only $($entries.Count) $($pair.Name) entries, below the $($pair.Min) floor"
        }
        $extracted = ($entries | ForEach-Object { @($_.text) + @($_.notes) }) -join ' '
        $sectionWords = Get-WordCount (ConvertTo-CleanText $pair.Section)
        $got = Get-WordCount $extracted
        if ($got -lt [math]::Ceiling($Config.WordCoverage * $sectionWords)) {
            throw "$slug`: $($pair.Name) extraction kept $got of $sectionWords words, below the $($Config.WordCoverage) coverage floor"
        }
        if ($pair.Name -eq 'tips') { $tips = $entries } else { $counters = $entries }
    }

    $crawlHeroes += [pscustomobject]@{
        slug = $slug; name = $h.name; tipSlug = $tipSlug
        fetchedAt = $fetchedAt; bytes = $page.Bytes.Length; accepted = $true
    }
    $derivedHeroes[$slug] = [pscustomobject]@{
        name = $h.name; tipSlug = $tipSlug; url = $url; fetchedAt = $fetchedAt
        tips = $tips; counters = $counters
    }
    Write-Host ("{0,-18} {1,3} tips {2,3} counters {3,6:N1}KB" -f $slug, $tips.Count, $counters.Count, ($page.Bytes.Length / 1KB))
    Start-Sleep -Milliseconds $Config.RequestIntervalMs
}

$stamp = & $now
@{ fetchedAt = $stamp; source = "$base/"; patch = $patch; names = $liveNames } |
    ConvertTo-Json -Depth 4 | Set-Content (Join-Path $Config.RawDir '_heroes.json') -Encoding utf8
@{ crawledAt = $stamp; source = "$base/tips/{tipSlug}.html"; patch = $patch; heroes = $crawlHeroes } |
    ConvertTo-Json -Depth 5 | Set-Content (Join-Path $Config.RawDir '_crawl.json') -Encoding utf8
@{
    fetchedAt = $stamp
    patch     = $patch
    source    = "$base/tips/{tipSlug}.html"
    note      = "Tsunami's hero tips and counters, extracted structure; the site's own content and license, see ref/dota2/README.md"
    heroes    = $derivedHeroes
} | ConvertTo-Json -Depth 8 | Set-Content (Join-Path $Config.Dir 'howdoiplay.json') -Encoding utf8
Write-Host "wrote $($pool.Count) raw dumps, _heroes.json, _crawl.json, howdoiplay.json (patch $patch)"
