param([int]$CapGB = 0)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$log = Join-Path $root 'var/memguard.log'
$stop = Join-Path $root 'var/memguard.stop'
New-Item -ItemType Directory -Force (Split-Path -Parent $log) | Out-Null
Remove-Item $stop -ErrorAction SilentlyContinue

$ram = [math]::Round((Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory / 1GB)
if ($CapGB -lt 1) { $CapGB = [math]::Max(1, [math]::Round($ram / 4)) }
"start ram=${ram}GB cap=${CapGB}GB pid=$PID" | Add-Content $log

function Get-ProjectProcs {
  Get-CimInstance Win32_Process | Where-Object {
    $_.CommandLine -and $_.ProcessId -ne $PID -and (
      $_.CommandLine -like '*dota-helper*' -or $_.CommandLine -like '*poolguide*')
  }
}

while (-not (Test-Path $stop)) {
  try {
    $procs = @(Get-ProjectProcs)
    $sum = ($procs | Measure-Object WorkingSetSize -Sum).Sum
    $sumGB = [math]::Round($sum / 1GB, 2)
    if ($sumGB -gt $CapGB) {
      $off = $procs | Sort-Object WorkingSetSize -Descending | Select-Object -First 1
      "breach ${sumGB}GB > ${CapGB}GB, killing pid $($off.ProcessId) $($off.Name) ws $([math]::Round($off.WorkingSetSize / 1GB, 2))GB" | Add-Content $log
      taskkill /PID $off.ProcessId /T /F 2>$null | Add-Content $log
      Start-Sleep -Seconds 2
      if (Get-Process -Id $off.ProcessId -ErrorAction SilentlyContinue) {
        "verify failed pid $($off.ProcessId) still alive" | Add-Content $log
      } else {
        "verify ok pid $($off.ProcessId) gone" | Add-Content $log
      }
    }
  } catch {
    "error $($_.Exception.Message)" | Add-Content $log
  }
  Start-Sleep -Seconds 15
}
"stop" | Add-Content $log
