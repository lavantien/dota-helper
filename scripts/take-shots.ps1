# take-shots.ps1: retake the two readme screenshots with headless chrome.
# the pages carry shot bootstraps (picker query params, guide hero hash) so
# the captures reproduce the readme frames without driving the UI. loopback
# static server only, same one make serve runs.
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
    Url = "http://127.0.0.1:$Port/guide/index.html#phantom-assassin"
    Size = '1440,900'
    Out = 'docs/guide.png'
  }
)

$server = Start-Process python -ArgumentList @('scripts/serve.py', "$Port") -PassThru -WindowStyle Hidden
try {
  $base = "http://127.0.0.1:$Port/picker/picker.html"
  $up = $false
  foreach ($i in 1..30) {
    if ($server.HasExited) { throw "serve.py exited early with $($server.ExitCode)" }
    try { Invoke-WebRequest $base -UseBasicParsing -TimeoutSec 2 | Out-Null; $up = $true; break } catch {}
    Start-Sleep -Milliseconds 500
  }
  if (-not $up) { throw "static server never came up on port $Port" }

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
}
