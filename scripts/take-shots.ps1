param([int]$Port = 8631)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
Set-Location $root

# chrome first, edge (chromium, ships with windows) as the fallback
$browser = @(
  "$env:ProgramFiles\Google\Chrome\Application\chrome.exe",
  "${env:ProgramFiles(x86)}\Google\Chrome\Application\chrome.exe",
  "$env:LOCALAPPDATA\Google\Chrome\Application\chrome.exe",
  "${env:ProgramFiles(x86)}\Microsoft\Edge\Application\msedge.exe",
  "$env:ProgramFiles\Microsoft\Edge\Application\msedge.exe"
) | Where-Object { $_ -and (Test-Path $_) } | Select-Object -First 1
if (-not $browser) { throw 'no chrome or edge executable found' }

# the readme art direction: url, viewport, and output per shot, aligned with
# the alt text in readme.md
$shots = @(
  @{
    Url = "http://127.0.0.1:$Port/picker/picker.html?enemies=meepo,enigma&role=1&aim=allies:1"
    Size = '1440,1700'
    Out = 'docs/picker.png'
  },
  @{
    Url = "http://127.0.0.1:$Port/picker/picker.html?subset=main#subpools"
    Size = '1440,1200'
    Out = 'docs/subpools.png'
  },
  @{
    Url = "http://127.0.0.1:$Port/picker/picker.html#guide"
    Size = '1440,1200'
    Out = 'docs/guide.png'
  },
  @{
    Url = "http://127.0.0.1:$Port/picker/picker.html#stats"
    Size = '1440,2400'
    Out = 'docs/stats.png'
  }
)

$prevDb = $env:SUBSETS_DB
$shotsDb = Join-Path $env:TEMP 'dota-helper-shots-subsets.db'
Remove-Item -LiteralPath $shotsDb, "$shotsDb-journal" -ErrorAction SilentlyContinue
$env:SUBSETS_DB = $shotsDb
$server = Start-Process python -ArgumentList @('scripts/serve.py', "$Port") -PassThru -WindowStyle Hidden
if ($null -ne $prevDb) { $env:SUBSETS_DB = $prevDb } else { Remove-Item Env:\SUBSETS_DB -ErrorAction SilentlyContinue }
try {
  $base = "http://127.0.0.1:$Port/picker/picker.html"
  $up = $false
  foreach ($i in 1..30) {
    if ($server.HasExited) { throw "serve.py exited early with $($server.ExitCode)" }
    try { Invoke-WebRequest $base -UseBasicParsing -TimeoutSec 2 | Out-Null; $up = $true; break } catch {}
    Start-Sleep -Milliseconds 500
  }
  if (-not $up) { throw "static server never came up on port $Port" }

  $api = "http://127.0.0.1:$Port/api/subsets"
  $mainPairs = @(
    @{ slug = 'slark'; role = '1' }, @{ slug = 'lifestealer'; role = '1' },
    @{ slug = 'lone-druid'; role = '1' }, @{ slug = 'natures-prophet'; role = '1' },
    @{ slug = 'necrophos'; role = '1' }, @{ slug = 'slark'; role = '2' },
    @{ slug = 'dragon-knight'; role = '2' }, @{ slug = 'necrophos'; role = '2' },
    @{ slug = 'necrophos'; role = '3' }, @{ slug = 'enigma'; role = '3' },
    @{ slug = 'tidehunter'; role = '3' }, @{ slug = 'slark'; role = '3' },
    @{ slug = 'mirana'; role = '4' }, @{ slug = 'hoodwink'; role = '4' },
    @{ slug = 'windranger'; role = '4' }, @{ slug = 'ogre-magi'; role = '4' },
    @{ slug = 'mirana'; role = '5' }, @{ slug = 'hoodwink'; role = '5' },
    @{ slug = 'windranger'; role = '5' }, @{ slug = 'ogre-magi'; role = '5' }
  ) | Sort-Object slug, role
  $created = Invoke-RestMethod -Method Post -Uri $api -ContentType 'application/json' -Body (@{ name = 'main' } | ConvertTo-Json)
  Invoke-RestMethod -Method Put -Uri "$api/$($created.id)" -ContentType 'application/json' `
    -Body (@{ entries = @($mainPairs) } | ConvertTo-Json -Depth 4) | Out-Null

  Add-Type -AssemblyName System.Drawing
  foreach ($s in $shots) {
    $out = Join-Path $root $s.Out
    # --virtual-time-budget holds the load open until the cdn icons resolve;
    # chrome writes its bytes-written line to stderr, both streams discarded
    & $browser --headless=new --disable-gpu --window-size=$($s.Size) `
      --screenshot="$out" --virtual-time-budget=20000 $s.Url 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0 -or -not (Test-Path $out)) { throw "capture failed for $($s.Out)" }
    $bytes = [IO.File]::ReadAllBytes($out)
    if ($bytes.Length -lt 8 -or $bytes[0] -ne 0x89 -or $bytes[1] -ne 0x50) { throw "$($s.Out) is not a png" }
    $want = $s.Size.Split(',')
    $img = [System.Drawing.Image]::FromFile($out)
    try {
      if ($img.Width -ne [int]$want[0] -or $img.Height -ne [int]$want[1]) {
        throw "$($s.Out) is $($img.Width)x$($img.Height), expected $($s.Size)"
      }
    } finally { $img.Dispose() }
    Write-Host "ok: $($s.Out) at $($s.Size)"
  }
} finally {
  if (-not $server.HasExited) { Stop-Process -Id $server.Id -Force }
  Remove-Item -LiteralPath $shotsDb, "$shotsDb-journal" -ErrorAction SilentlyContinue
}
