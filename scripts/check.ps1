#Requires -Version 7
# Static verification for the pick simulator pipeline and the guide. Runs via `make check`.
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$fail = @()

# heroes with dotabuff dumps on disk: the previous pool, frozen reference only
# (the fallback tier is retired, stratz is the exclusive scoring source). the
# current pool comes from config alone.
$dumpSlugs = @('arc-warden', 'bloodseeker', 'dark-seer', 'doom', 'dragon-knight',
               'hoodwink', 'kunkka', 'lifestealer', 'lone-druid', 'ogre-magi',
               'spectre', 'spirit-breaker', 'tiny', 'undying', 'visage',
               'warlock', 'windranger', 'winter-wyvern')

# files that must exist
$expected = @(
    'Makefile',
    '.gitignore',
    'scripts/serve.py',
    'config.json',
    'content.json',
    'picker/gates.json',
    'picker/DATA-CONTRACT.md',
    'engine/go.mod',
    'engine/cmd/engine/main.go',
    'guide/index.html',
    'guide/data.js',
    'guide/guide-data-generated.js',
    'ref/dota2/matches/raw/_backfill.json',
    'ref/dota2/positions/raw/_positions.json',
    'ref/dota2/trends/snapshots.ndjson',
    'ref/dota2/README.md',
    'ref/dota2/patch/7.41-series.md'
)
foreach ($s in $dumpSlugs) { $expected += "ref/dota2/matchups/$s.md"; $expected += "ref/dota2/matchups/raw/$s.txt" }
foreach ($rel in $expected) {
    if (-not (Test-Path (Join-Path $root $rel))) { $fail += "missing: $rel" }
}
if ($fail.Count -eq 0) { Write-Host "ok: all $($expected.Count) expected files exist" }

# config hub parses and drives every derived count
$cfg = Get-Content (Join-Path $root 'config.json') -Raw | ConvertFrom-Json
$poolUnique = @($cfg.pool | ForEach-Object { $_.slug } | Sort-Object -Unique)
$poolEntries = @($cfg.pool).Count
$multiRole = @($cfg.pool | Group-Object slug | Where-Object { $_.Count -gt 1 })
$extraSlots = ($multiRole | ForEach-Object { $_.Count - 1 } | Measure-Object -Sum).Sum
if ($null -eq $extraSlots) { $extraSlots = 0 }
if ($poolEntries -ne ($poolUnique.Count + $extraSlots)) {
    $fail += "config pool: $poolEntries entries over $($poolUnique.Count) unique heroes does not match the multi-role slot count $extraSlots"
}
foreach ($g in $multiRole) {
    $roles = @($g.Group.role | Sort-Object -Unique)
    if ($roles.Count -ne $g.Count) { $fail += "config pool: hero $($g.Name) has duplicate role entries" }
}
$content = Get-Content (Join-Path $root 'content.json') -Raw | ConvertFrom-Json
foreach ($slug in $poolUnique) {
    if (-not $content.heroes.PSObject.Properties[$slug]) { $fail += "content.json has no prose for pool hero $slug" }
}
$contentSlugs = @($content.heroes.PSObject.Properties.Name | Sort-Object) -join ' '
if ($contentSlugs -ne ($poolUnique -join ' ')) { $fail += 'content.json hero set differs from config pool' }

# framework completeness: every pool hero carries the full why/how/when prose,
# at least one well formed timing and tech line, and the drills cover the
# whole pool. pwsh @($null).Count is 1, so every count gate also checks null
# and entry shape explicitly.
foreach ($slug in $poolUnique) {
    $h = $content.heroes.PSObject.Properties[$slug].Value
    foreach ($f in 'identity', 'why', 'how', 'when', 'build') {
        if ($null -eq $h.$f -or $h.$f -isnot [string] -or -not $h.$f.Trim()) { $fail += "content.json: $slug has empty $f" }
    }
    if ($null -eq $h.timings -or @($h.timings).Count -lt 1) { $fail += "content.json: $slug has no timings" }
    else {
        foreach ($t in @($h.timings)) {
            if ($null -eq $t -or @($t).Count -lt 2 -or $null -eq $t[0] -or $t[0] -isnot [string] -or -not $t[0].Trim() -or
                $null -eq $t[1] -or $t[1] -isnot [ValueType]) {
                $fail += "content.json: $slug has a malformed timing entry"
            }
        }
    }
    if ($null -eq $h.tech -or @($h.tech).Count -lt 1) { $fail += "content.json: $slug has no tech lines" }
    else {
        foreach ($line in @($h.tech)) {
            if ($null -eq $line -or $line -isnot [string] -or -not $line.Trim()) { $fail += "content.json: $slug has a blank tech line" }
        }
    }
    if ($null -eq $h.mechanics -or @($h.mechanics).Count -lt 1) { $fail += "content.json: $slug has no mechanics lines" }
    else {
        foreach ($m in @($h.mechanics)) {
            if ($m -isnot [array] -or @($m).Count -ne 2 -or $m[0] -notin 'tip', 'counter' -or
                $null -eq $m[1] -or $m[1] -isnot [string] -or -not $m[1].Trim()) {
                $fail += "content.json: $slug has a malformed mechanics entry"
            }
        }
    }
    # first-principles prose: no numerals in the description fields beyond the
    # allowlisted kit facts (pos N, level N, XvX). numbers live in the stratz
    # synced build/timings and the generated data tables, never in prose
    $proseNum = 'pos \d+|level \d+|\d+v\d+'
    foreach ($f in 'identity', 'why', 'how', 'when') {
        if ($h.$f -is [string] -and ($h.$f -replace $proseNum, '') -match '\d') {
            $fail += "content.json: $slug.$f carries an unbacked number, first-principles prose only"
        }
    }
    foreach ($line in @($h.tech)) {
        if ($line -is [string] -and ($line -replace $proseNum, '') -match '\d') {
            $fail += "content.json: $slug tech carries an unbacked number, first-principles prose only"
        }
    }
}
$drills = @($content.practice.drills)
if ($null -eq $content.practice.drills) { $drills = @() }
if ($drills.Count -ne $poolUnique.Count) { $fail += "content.json: $($drills.Count) drill entries does not match the $($poolUnique.Count) hero pool" }
$drillSlugs = @($drills | ForEach-Object { $_[0] } | Sort-Object -Unique)
if (($drillSlugs -join ' ') -ne ($poolUnique -join ' ')) { $fail += 'content.json: drill hero set differs from config pool' }
foreach ($d in $drills) {
    if ($d -isnot [array] -or @($d).Count -lt 2) { $fail += 'content.json: malformed drill entry, want [slug, text]'; continue }
    if ($null -eq $d[1] -or $d[1] -isnot [string] -or -not $d[1].Trim()) { $fail += "content.json: drill for $($d[0]) is empty" }
}
if ($fail.Count -eq 0) { Write-Host "ok: framework prose complete for $($poolUnique.Count) pool heroes, drills cover all of them" }

# builds crawl: manifest and per-entry dumps for every pool (hero, role) pair,
# patch series in step with the config hub, and the synced prose carries the
# buildsMeta stamp of the same crawl
$buildsRaw = $cfg.paths.buildsRawDir
$buildsOk = $true
foreach ($rel in @("$buildsRaw/_crawl.json", "$buildsRaw/_items.json")) {
    if (-not (Test-Path (Join-Path $root $rel))) { $fail += "missing: $rel"; $buildsOk = $false }
}
if ($buildsOk) {
    $bc = Get-Content (Join-Path $root "$buildsRaw/_crawl.json") -Raw | ConvertFrom-Json
    $cfgSeries = $cfg.patch -replace '[a-zA-Z]+$', ''
    if ($bc.series -ne $cfgSeries) { $fail += "builds crawl series $($bc.series) differs from config patch series $cfgSeries, rerun make fetch-builds" }
    foreach ($e in @($cfg.pool)) {
        if (-not (Test-Path (Join-Path $root "$buildsRaw/$($e.slug)@$($e.role).json"))) {
            $fail += "builds dump missing for $($e.slug)@$($e.role)"
        }
    }
    # the full meta string is reconstructed from the manifest: a fresh crawl
    # without a sync (or a stale date) must not slip through on the series alone
    $fetchedDate = ([datetime]$bc.crawledAt).ToUniversalTime().ToString('yyyy-MM-dd')
    $wantMeta = "stratz $($bc.field) completed week $($bc.week), $($bc.bracket.ToLower()), fetched $fetchedDate, series $($bc.series)"
    if ($content.buildsMeta -isnot [string] -or $content.buildsMeta -ne $wantMeta) {
        $fail += "content.json buildsMeta out of step with the crawl, rerun make sync-builds (want: $wantMeta)"
    }
    if ($fail.Count -eq 0) { Write-Host "ok: builds crawl covers all $(@($cfg.pool).Count) pool entries, series $($bc.series), prose synced" }

    # builds vocabulary and drop accounting: prose may only name items the
    # hero's own synced build carries (counter-play vocabulary from the hub
    # excepted), timing labels must come from the build, and every pooled role
    # is either covered by the build or recorded as dropped in _sync.json
    $bi = Get-Content (Join-Path $root "$buildsRaw/_items.json") -Raw | ConvertFrom-Json
    $contextItems = @()
    if ($cfg.builds.proseContextItems) { $contextItems = @($cfg.builds.proseContextItems | ForEach-Object { $_.ToLower() }) }
    $stopWords = @()
    if ($cfg.builds.proseGateStopWords) { $stopWords = @($cfg.builds.proseGateStopWords | ForEach-Object { $_.ToLower() }) }
    $dict = @{}
    foreach ($it in @($bi.items)) {
        foreach ($n in @(($it.shortName -replace '_', ' '), "$($it.displayName)".ToLower())) {
            if ($n.Length -ge 4 -and $stopWords -notcontains $n) { $dict[$n] = $true }
        }
    }
    if ($cfg.builds.itemAliases) {
        foreach ($a in $cfg.builds.itemAliases.PSObject.Properties) {
            $v = "$($a.Value)".ToLower()
            if ($v -and $stopWords -notcontains $v) { $dict[$v] = $true }
        }
    }
    $dictRx = @{}
    foreach ($phrase in $dict.Keys) {
        $w = $phrase.Split(' ')
        $last = [regex]::Escape($w[-1]) + '(?:es|s)?'
        if ($w.Count -eq 1) { $dictRx[$phrase] = "\b$last\b" }
        else {
            $pre = @($w[0..($w.Count - 2)] | ForEach-Object { [regex]::Escape($_) }) -join '\s+'
            $dictRx[$phrase] = "\b$pre\s+$last\b"
        }
    }
    $syncOk = $false
    $syncPath = Join-Path $root "$buildsRaw/_sync.json"
    if (Test-Path $syncPath) {
        $bs = Get-Content $syncPath -Raw | ConvertFrom-Json
        $syncOk = $true
        if ($bs.series -ne $cfgSeries) { $fail += "builds _sync.json series $($bs.series) differs from config series $cfgSeries, rerun make sync-builds" }
    }
    else { $fail += "missing: $buildsRaw/_sync.json (run make sync-builds)" }

    foreach ($slug in $poolUnique) {
        $h = $content.heroes.PSObject.Properties[$slug].Value
        $allowed = [System.Collections.Generic.HashSet[string]]::new()
        foreach ($seg in (@($h.build) -split '\. ')) {
            $s = ($seg -replace '^pos [0-9/]+:\s*', '').Trim()
            foreach ($tok in ($s -split ', ')) {
                $t = $tok.Trim().ToLower()
                if ($t) { [void]$allowed.Add($t) }
            }
        }
        foreach ($t in @($h.timings)) {
            if ($t -and $t[0]) {
                $lbl = "$($t[0])".ToLower()
                if ($h.build -notmatch [regex]::Escape($lbl)) { $fail += "content.json: $slug timing '$lbl' is not in its build" }
                [void]$allowed.Add($lbl)
            }
        }
        if ($syncOk) {
            $cfgRoles = @($cfg.pool | Where-Object slug -eq $slug | ForEach-Object role | Sort-Object -Unique)
            $e = $bs.heroes.PSObject.Properties[$slug].Value
            if (-not $e) { $fail += "builds _sync.json has no entry for $slug, rerun make sync-builds" }
            else {
                $covered = @($e.roles | Sort-Object -Unique)
                $dropped = @($e.dropped | Sort-Object -Unique)
                if ($covered.Count -eq 0) { $fail += "builds _sync.json: $slug covers no pooled role" }
                if (((@($covered) + @($dropped) | Sort-Object -Unique) -join ' ') -ne ($cfgRoles -join ' ')) {
                    $fail += "builds _sync.json: $slug roles+dropped do not account for pooled roles $($cfgRoles -join '/')"
                }
            }
        }
        $lines = @($h.identity, $h.why, $h.how, $h.when) + @($h.tech)
        foreach ($d in @($content.practice.drills)) { if ($d[0] -eq $slug) { $lines += "$($d[1])" } }
        $allowedPlus = @($allowed) + $contextItems
        foreach ($phrase in $dict.Keys) {
            $skip = $false
            foreach ($a in $allowedPlus) { if ($a -like "*$phrase*") { $skip = $true; break } }
            if ($skip) { continue }
            $rx = $dictRx[$phrase]
            foreach ($line in $lines) {
                if ($line -is [string] -and $line -match $rx) {
                    $fail += "content.json: $slug prose names item '$phrase' but its build does not carry it"
                    break
                }
            }
        }
    }
    if ($fail.Count -eq 0) { Write-Host "ok: prose item vocabulary, timing labels, and role drops all accounted" }
}

# howdoiplay snapshot: metadata, derived extraction, and raw dumps for every
# pool hero; hero set and patch series must agree with the config hub
$hdpRaw = 'ref/dota2/howdoiplay/raw'
foreach ($rel in @("$hdpRaw/_heroes.json", "$hdpRaw/_crawl.json", 'ref/dota2/howdoiplay/howdoiplay.json')) {
    if (-not (Test-Path (Join-Path $root $rel))) { $fail += "missing: $rel" }
}
foreach ($slug in $poolUnique) {
    if (-not (Test-Path (Join-Path $root "$hdpRaw/$slug.html"))) { $fail += "missing: $hdpRaw/$slug.html" }
}
$hdpPath = Join-Path $root 'ref/dota2/howdoiplay/howdoiplay.json'
if (Test-Path $hdpPath) {
    $hdp = Get-Content $hdpPath -Raw | ConvertFrom-Json
    $hdpSlugs = @($hdp.heroes.PSObject.Properties.Name | Sort-Object)
    if (($hdpSlugs -join ' ') -ne ($poolUnique -join ' ')) {
        $fail += 'howdoiplay.json hero set differs from config pool'
    } else {
        foreach ($slug in $hdpSlugs) {
            $h = $hdp.heroes.PSObject.Properties[$slug].Value
            if ($null -eq $h.tips -or @($h.tips).Count -lt 1 -or $null -eq $h.tips[0].text) { $fail += "howdoiplay.json: $slug has no tips" }
            if ($null -eq $h.counters -or @($h.counters).Count -lt 1 -or $null -eq $h.counters[0].text) { $fail += "howdoiplay.json: $slug has no counters" }
        }
        $cfgSeries = $cfg.patch -replace '[a-z]$', ''
        if ($hdp.patch -ne $cfgSeries) { $fail += "howdoiplay.json patch $($hdp.patch) differs from config patch series $cfgSeries, rerun make fetch-howdoiplay" }
        if ($fail.Count -eq 0) { Write-Host "ok: howdoiplay snapshot covers $($poolUnique.Count) pool heroes, patch $($hdp.patch)" }
    }
}

# curated provenance: every pool hero's mechanics lines carry refs back into
# the howdoiplay extraction, and both sides of every ref resolve
$curPath = Join-Path $root 'ref/dota2/howdoiplay/curated.json'
if (-not (Test-Path $curPath)) {
    $fail += 'missing: ref/dota2/howdoiplay/curated.json'
} elseif (Test-Path $hdpPath) {
    $cur = Get-Content $curPath -Raw | ConvertFrom-Json
    $curSlugs = @($cur.heroes.PSObject.Properties.Name | Sort-Object)
    if (($curSlugs -join ' ') -ne ($poolUnique -join ' ')) {
        $fail += 'curated.json hero set differs from config pool'
    } else {
        if ($cur.patch -ne $hdp.patch) { $fail += "curated.json patch $($cur.patch) differs from extraction patch $($hdp.patch)" }
        $refRx = '^(tips|counters)\[(\d+)\](?:\.notes\[(\d+)\])?$'
        foreach ($slug in $curSlugs) {
            $mech = $content.heroes.PSObject.Properties[$slug].Value.mechanics
            $src = $hdp.heroes.PSObject.Properties[$slug].Value
            if ($null -eq $mech) { continue }
            # bijection: every mechanics line carries exactly one ref, no orphans either way
            $curIdxs = @($cur.heroes.PSObject.Properties[$slug].Value | ForEach-Object { [int]$_.idx } | Sort-Object)
            $wantIdxs = @(0..(@($mech).Count - 1))
            if (($curIdxs -join ' ') -ne ($wantIdxs -join ' ')) {
                $fail += "curated.json: $slug provenance refs ($($curIdxs -join ' ')) do not cover every mechanics line exactly once (0..$($wantIdxs[-1]))"; continue
            }
            foreach ($e in @($cur.heroes.PSObject.Properties[$slug].Value)) {
                if ($null -eq $e.idx -or [int]$e.idx -ge @($mech).Count) {
                    $fail += "curated.json: $slug idx $($e.idx) out of mechanics range"; continue
                }
                if ($null -eq $e.from -or $e.from -notmatch $refRx) {
                    $fail += "curated.json: $slug has malformed from ref '$($e.from)'"; continue
                }
                # frozen contract: every mechanics line carries a verified verdict
                # covering exactly its committed text
                $v = $e.verified
                if ($null -eq $v -or -not "$($v.verdict)" -or $v.verdict -notin 'confirmed', 'corrected' -or
                    -not "$($v.date)" -or -not "$($v.source)" -or "$($v.text)" -ne "$($mech[[int]$e.idx][1])") {
                    $fail += "curated.json: $slug idx $($e.idx) has no verified verdict covering its mechanics text"
                }
                # tags classify the line for the reader (tip vs counter for the
                # hero's player), from refs record where the text was mined;
                # enemy-facing counter bullets legitimately become player tips,
                # so no family consistency is asserted here
                $sec, $n, $note = $Matches[1], [int]$Matches[2], $Matches[3]
                $base = @($src.$sec)
                if ($n -ge $base.Count) { $fail += "curated.json: $slug from ref '$($e.from)' out of $sec range"; continue }
                if ($note) {
                    if ($null -eq $base[$n].notes -or [int]$note -ge @($base[$n].notes).Count) {
                        $fail += "curated.json: $slug from ref '$($e.from)' out of notes range"
                    }
                }
            }
        }
        if ($fail.Count -eq 0) { Write-Host "ok: curated mechanics provenance resolves for $($poolUnique.Count) pool heroes" }
    }
}

# roster slugs derived from the emitted guide data, never hardcoded
$guideGen = Get-Content (Join-Path $root 'guide/guide-data-generated.js') -Raw
$rosterSlugs = @([regex]::Matches($guideGen, "^\s+'([a-z0-9-]+)':\s*", 'Multiline') |
    ForEach-Object { $_.Groups[1].Value } | Sort-Object -Unique)

# gates reference pool heroes only, condition heroes must be roster slugs,
# rule roles must be real role ids the target actually plays, and each
# fallbackOrder list must cover exactly the pool entries of that role, in the
# same order (both derive from make order-pool)
$gates = Get-Content (Join-Path $root 'picker/gates.json') -Raw | ConvertFrom-Json
$roleIds = @($cfg.roles | ForEach-Object { $_.id })
$poolRoles = @{}
foreach ($e in $cfg.pool) { $poolRoles[$e.slug] = @($poolRoles[$e.slug]) + $e.role }
foreach ($r in @($gates.rules) + @($gates.autogenerated)) {
    if ($poolUnique -notcontains $r.target) { $fail += "gates rule $($r.id) targets non-pool hero $($r.target)"; continue }
    foreach ($role in @($r.roles)) {
        if ($roleIds -notcontains $role) { $fail += "gates rule $($r.id) has unknown role $role" }
        elseif ($poolRoles[$r.target] -notcontains $role) { $fail += "gates rule $($r.id) scopes $($r.target) to role $role it does not play" }
    }
    foreach ($cond in @($r.when)) {
        foreach ($h in @($cond.heroes)) {
            if ($h -and ($rosterSlugs -notcontains $h)) { $fail += "gates rule $($r.id) references unknown hero $h" }
        }
    }
}
foreach ($rid in $roleIds) {
    $want = @($cfg.pool | Where-Object role -eq $rid | ForEach-Object slug | Sort-Object -Unique)
    $got = @($gates.fallbackOrder.$rid | Sort-Object -Unique)
    if (($got -join ' ') -ne ($want -join ' ')) { $fail += "gates fallbackOrder role $rid differs from the config pool role set" }
    $wantOrdered = @($cfg.pool | Where-Object role -eq $rid | ForEach-Object slug)
    $gotOrdered = @($gates.fallbackOrder.$rid)
    if (($gotOrdered -join ' ') -ne ($wantOrdered -join ' ')) { $fail += "gates fallbackOrder role $rid order differs from the config pool declaration order (run make order-pool)" }
}

# the aoe clear set joins against roster slugs at score time, so a slug that
# misses the roster silently counts nothing (the magnus/magnataur bug class)
foreach ($s in @($cfg.aoeClearHeroes)) {
    if ($rosterSlugs -notcontains $s) { $fail += "config aoeClearHeroes carries non-roster slug $s" }
}

# derivation manifest: every scoring-affecting config key (weights,
# gateDeltas, shrink, completion, score, normalize) must carry a method, and
# the recorded value must stay in sync with the hub
$derivPath = Join-Path $root $cfg.paths.derivationsPath
if (-not (Test-Path $derivPath)) { $fail += "missing: $($cfg.paths.derivationsPath)" }
else {
    $deriv = Get-Content $derivPath -Raw | ConvertFrom-Json
    $byKey = @{}
    foreach ($e in @($deriv.entries)) { $byKey[$e.key] = $e }
    $missing = @()
    foreach ($sec in 'weights', 'gateDeltas', 'shrink', 'completion', 'score', 'normalize') {
        # map-valued keys (weights.synByRole) expand to one manifest leaf per sub-key
        $leaves = @()
        foreach ($p in $cfg.$sec.PSObject.Properties) {
            if ($p.Value -is [System.Management.Automation.PSCustomObject]) {
                foreach ($sp in $p.Value.PSObject.Properties) { $leaves += ,@("$sec.$($p.Name).$($sp.Name)", $sp.Value) }
            } else { $leaves += ,@("$sec.$($p.Name)", $p.Value) }
        }
        foreach ($leaf in $leaves) {
            $key = $leaf[0]
            $e = $byKey[$key]
            if (-not $e -or -not "$($e.method)") { $missing += $key; continue }
            if ([math]::Abs([double]$e.value - [double]$leaf[1]) -gt 1e-9) {
                $fail += "derivations: $key value $($e.value) differs from the config value $($leaf[1])"
            }
        }
    }
    if ($missing.Count -gt 0) { $fail += "derivation manifest missing entries or methods for: $($missing -join ', ')" }
    else { Write-Host 'ok: derivation manifest covers every scoring-affecting key in sync with the hub' }
}

# gate notes are ui prose: qualitative direction only, winrates and match
# counts live in the generated data tables
foreach ($r in @($gates.rules) + @($gates.autogenerated)) {
    if ("$($r.note)" -match '\d') { $fail += "gates rule $($r.id) note carries a numeral, qualitative notes only" }
}

# guide data.js mirrors the config pool both ways (no drift in the authored
# mirror: no pool hero missing, no non-pool hero lingering)
$dataJs = Get-Content (Join-Path $root 'guide/data.js') -Raw
foreach ($slug in $poolUnique) {
    if ($dataJs -notmatch [regex]::Escape("slug: '$slug'")) { $fail += "guide data.js missing pool hero $slug" }
}
$dataJsSlugs = [regex]::Matches($dataJs, "slug: '([a-z0-9-]+)'") | ForEach-Object { $_.Groups[1].Value } | Sort-Object -Unique
$extra = @($dataJsSlugs | Where-Object { $poolUnique -notcontains $_ })
if ($extra.Count) { $fail += "guide data.js carries non-pool hero slugs: $($extra -join ', ')" }
# retired content guard matches the structural `trees:` key at line start, not
# the word in prose (mechanics lines legitimately mention trees, e.g. sprout)
if ($dataJs -match '(?m)^\s*trees\s*:') { $fail += 'guide data.js still carries retired tree content' }

# fallback snapshots parse and cover every dumped hero
$current = Get-ChildItem (Join-Path $root 'ref/dota2/matchups') -Filter 'current-*.json' |
    Sort-Object Name | Select-Object -Last 1
if (-not $current) {
    $fail += 'missing: ref/dota2/matchups/current-*.json'
} else {
    $data = Get-Content $current.FullName -Raw | ConvertFrom-Json
    $n = ($data.heroes.PSObject.Properties | Measure-Object).Count
    if ($n -ne $dumpSlugs.Count) { $fail += "$($current.Name): expected $($dumpSlugs.Count) dumped heroes, got $n" }
    else { Write-Host "ok: $($current.Name) parses with $n previous-pool fallback heroes, mirror check $($data.mirror_check.pairs) pairs worst $($data.mirror_check.worst_divergence)" }
}
$od = Get-ChildItem (Join-Path $root 'ref/dota2/matchups') -Filter 'opendota-*.json' |
    Sort-Object Name | Select-Object -Last 1
if (-not $od) {
    $fail += 'missing: ref/dota2/matchups/opendota-*.json'
} else {
    $null = Get-Content $od.FullName -Raw | ConvertFrom-Json
    Write-Host "ok: $(Split-Path $od.Name -Leaf) parses"
}

# rebuilt fallback tables are German-locale Dotabuff sourced with 4-column rows
$rowRx = '^\|\s*[^|]+?\s*\|\s*-?[\d.]+\s*\|\s*-?[\d.]+\s*\|\s*\d+\s*\|\s*$'
foreach ($s in $dumpSlugs) {
    $p = Join-Path $root "ref/dota2/matchups/$s.md"
    if (-not (Test-Path $p)) { continue }
    $lines = Get-Content $p
    if ($lines[0] -notmatch 'matchup table') { $fail += "$s.md: bad title" }
    if (-not ($lines | Where-Object { $_ -like '*de.dotabuff.com*' })) { $fail += "$s.md: no German-locale source note" }
    if (-not ($lines | Where-Object { $_ -like '*Sanity:*' })) { $fail += "$s.md: no sanity note" }
    $rows = @($lines | Where-Object { $_ -match $rowRx })
    if ($rows.Count -lt 100) { $fail += "$s.md: only $($rows.Count) table rows" }
    $bad = @($lines | Where-Object { $_.StartsWith('|') -and $_ -notmatch $rowRx -and $_ -notmatch '^\|\s*(enemy|---)' })
    if ($bad.Count -gt 0) { $fail += "$s.md: $($bad.Count) malformed rows, first: $($bad[0])" }
}
if ($fail.Count -eq 0) { Write-Host "ok: $($dumpSlugs.Count) previous-pool fallback tables well formed" }

# SLOC cap on every source and doc file we ship
$slocTargets = @(
    'Makefile', 'scripts/fetch-matchups.ps1', 'scripts/build-guide-data.ps1', 'scripts/check.ps1',
    'scripts/fetch-howdoiplay.ps1', 'scripts/serve.py',
    'ui/ui.css', 'guide/guide.css', 'picker/picker.css',
    'guide/index.html', 'guide/data.js', 'guide/guide-data-generated.js',
    'readme.md', 'ref/dota2/README.md',
    'config.json', 'content.json',
    'picker/gates.json', 'picker/DATA-CONTRACT.md'
) + ($expected | Where-Object { $_ -like 'ref/dota2/matchups/*.md' })
$engineDir = Join-Path $root 'engine'
if (Test-Path $engineDir) {
    $slocTargets += @(Get-ChildItem $engineDir -Recurse -Include *.go, '*.mod' | ForEach-Object { $_.FullName })
}
$pickerDir = Join-Path $root 'picker'
if (Test-Path $pickerDir) {
    # generated data files are machine-written, the SLOC cap targets authored files
    $slocTargets += @(Get-ChildItem $pickerDir -Include *.js, *.mjs, *.html, *.css -Recurse |
        Where-Object { $_.Name -notlike '*-generated.js' } | ForEach-Object { $_.FullName })
}
# per-file overrides keyed by repo-relative path: authored pool data grows with
# the pool and its config hub, config/content/gates carry a higher cap than the
# blanket 500; config.json is machine-rewritten by `eval promote` (element per
# line), so its cap tracks the hub's grown size, not hand-formatting; guide
# data.js is engine-emitted pool data (one block per pool entry) and grows the
# same way despite not carrying a -generated suffix; gates.json carries the
# per-role fallback order plus one authored rule set per hero and passed 600
# with the v0.4 freeze (44 rules over 44 heroes); config.go is that hub's
# loader, one struct and one validate arm per hub section, so it tracks the
# same growth; check.ps1 grows the same way, one gate per pass that touches
# the pool or its derived surfaces
$slocCapFor = @{ 'config.json' = 900; 'content.json' = 1300; 'picker\gates.json' = 700; 'guide\data.js' = 700; 'engine\internal\config\config.go' = 550; 'scripts\check.ps1' = 550 }
$seen = @{}
$raised = 0
foreach ($t in $slocTargets) {
    $full = if ([IO.Path]::IsPathRooted($t)) { $t } else { Join-Path $root $t }
    if (-not (Test-Path $full) -or $seen[$full]) { continue }
    $seen[$full] = $true
    $n = (Get-Content $full | Measure-Object -Line).Lines
    $rel = if ($full.StartsWith($root)) { $full.Substring($root.Length + 1) } else { $full }
    $cap = $slocCapFor[$rel -replace '/', '\']
    if ($null -eq $cap) { $cap = 500 } else { $raised++ }
    if ($n -gt $cap) { $fail += "$(Split-Path $full -Leaf): $n lines exceeds $cap SLOC cap" }
}
if ($fail.Count -eq 0) { Write-Host "ok: $($seen.Count) files within their SLOC caps, $raised on raised caps" }

# scan surface for attribution and token leaks (this script defines the
# patterns, skip itself). enumerate the git-tracked set plus fresh untracked
# files, never an extension allowlist, so a token pasted into any file class a
# commit can ship trips the gate by construction (var/ is gitignored, the dir
# regex is the backstop). fail closed: an empty surface means the enumeration
# broke, not that the tree is clean
$scanList = git -C $root ls-files --cached --others --exclude-standard
if ($LASTEXITCODE -ne 0 -or @($scanList).Count -eq 0) {
    $fail += 'leak scan: git ls-files returned no scan surface'
}
# -Force: on unix pwsh every dot-prefixed path is Hidden and Get-Item skips
# hidden items by default, so without it the leak gate never opens dotfiles
$scanFiles = @($scanList |
    Where-Object { $_ -notmatch '(^|[/\\])(\.git|\.claude|var|node_modules)([/\\]|$)' -and (Split-Path $_ -Leaf) -ne 'check.ps1' } |
    ForEach-Object { Get-Item -LiteralPath (Join-Path $root $_) -Force })
$attribution = 'Co-Authored-By', 'Generated with Claude'
foreach ($f in $scanFiles) {
    $hit = Select-String -Path $f.FullName -Pattern ($attribution -join '|')
    if ($hit) { $fail += "attribution string in $($f.FullName): $($hit[0].Line)" }
}
if ($fail.Count -eq 0) { Write-Host 'ok: no attribution strings' }

# authored prose must stay complete: no placeholder markers in any tracked file
foreach ($f in $scanFiles) {
    $hit = Select-String -Path $f.FullName -Pattern 'TODO\('
    if ($hit) { $fail += "placeholder marker in $($f.FullName): $($hit[0].Line)" }
}
if ($fail.Count -eq 0) { Write-Host 'ok: no placeholder markers' }

# jwt-shaped api tokens must never live in tracked files (var/ holds the real one)
$jwt = ('e' + 'yJ') + '[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}'
foreach ($f in $scanFiles) {
    $hit = Select-String -Path $f.FullName -Pattern $jwt
    if ($hit) { $fail += "token-like literal in $($f.FullName): $($hit[0].Filename)" }
}
if ($fail.Count -eq 0) { Write-Host 'ok: no token-like literals' }

if ($fail.Count -gt 0) {
    $fail | ForEach-Object { Write-Host "FAIL: $_" }
    exit 1
}
Write-Host 'all checks passed'
