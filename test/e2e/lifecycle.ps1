# Process lifecycle checks on Windows (no Administrator needed): stop, restart
# on crash with backoff, giving up, and cancelling a pending restart.
#
#   .\test\e2e\lifecycle.ps1 -Morrow <morrow.exe>
param([Parameter(Mandatory)] [string] $Morrow)
$ErrorActionPreference = 'Stop'

$home_ = Join-Path ([IO.Path]::GetTempPath()) ('morrow-lifecycle-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $home_ | Out-Null
$env:MORROW_HOME = $home_
$m = Join-Path $home_ 'morrow.exe'
Copy-Item $Morrow $m
# A unique ping count marks the test's own processes.
$marker = '10007'

function Status($app) { ((& $m status $app) | Select-String '^Status: (.*)').Matches[0].Groups[1].Value }
function PidOf($app) { [int]((& $m status $app) | Select-String '^PID: (\d+)').Matches[0].Groups[1].Value }
function Alive($procId) { [bool](Get-Process -Id $procId -ErrorAction SilentlyContinue) }
function Strays { @(Get-CimInstance Win32_Process -Filter "Name = 'PING.EXE'" | Where-Object { $_.CommandLine -match $marker }) }
$script:pass = 0; $script:fail = 0
function Ok([string] $name, [bool] $cond) {
    if ($cond) { $script:pass++; Write-Host "PASS  $name" } else { $script:fail++; Write-Host "FAIL  $name" }
}

try {
    & $m init | Out-Null
    & $m create plain ping -n $marker 127.0.0.1 | Out-Null
    & $m create --restart unless-stopped crashy ping -n $marker 127.0.0.1 | Out-Null
    & $m create --restart unless-stopped --max-restarts 2 loop cmd /c exit 3 | Out-Null

    Write-Host 'stop:'
    & $m start plain | Out-Null; Start-Sleep -Milliseconds 500; $p = PidOf plain
    $t = [Diagnostics.Stopwatch]::StartNew(); & $m stop plain | Out-Null; $t.Stop()
    Ok "stops a windowless app ($($t.ElapsedMilliseconds)ms)" ($t.ElapsedMilliseconds -lt 8000 -and -not (Alive $p))
    Ok 'recorded as stopped, not restarted' ((Status plain) -eq 'stopped')

    Write-Host 'restart on crash:'
    & $m start crashy | Out-Null; Start-Sleep -Milliseconds 500; $p1 = PidOf crashy
    Stop-Process -Id $p1 -Force; Start-Sleep -Milliseconds 2500; $p2 = PidOf crashy
    Ok "killed app is restarted with a new PID ($p1 -> $p2)" ((Status crashy) -eq 'running' -and $p2 -ne $p1 -and (Alive $p2))
    Ok 'log explains the restart' ([bool]((& $m logs crashy) | Select-String 'restarting in 1s \(attempt 1/5\)'))
    Stop-Process -Id $p2 -Force; Start-Sleep -Milliseconds 500
    Ok "status is 'restarting' during the backoff" ((Status crashy) -eq 'restarting')
    & $m stop crashy | Out-Null; Start-Sleep -Seconds 3
    Ok 'stop during the backoff cancels the restart' ((Status crashy) -eq 'stopped' -and [bool]((& $m logs crashy) | Select-String 'restart cancelled'))

    Write-Host 'crash loop:'
    & $m start loop | Out-Null; Start-Sleep -Seconds 6
    Ok 'gives up after --max-restarts' ((Status loop) -eq 'exited' -and [bool]((& $m detail loop) | Select-String 'restarted 2'))

    Write-Host 'policy no:'
    & $m start plain | Out-Null; Start-Sleep -Milliseconds 500
    Stop-Process -Id (PidOf plain) -Force; Start-Sleep -Milliseconds 1500
    Ok 'unexpected exit is recorded as exited' ((Status plain) -eq 'exited')

    Write-Host 'restart command:'
    & $m start crashy | Out-Null; Start-Sleep -Milliseconds 500; $p1 = PidOf crashy
    & $m restart crashy | Out-Null; Start-Sleep -Milliseconds 500; $p2 = PidOf crashy
    Ok "restart replaces the process ($p1 -> $p2)" ((Status crashy) -eq 'running' -and $p2 -ne $p1 -and -not (Alive $p1))
    & $m stop crashy | Out-Null

    Write-Host 'cleanup:'
    Ok 'no stray app processes' ((Strays).Count -eq 0)

    Write-Host "`nresult: $($script:pass) passed, $($script:fail) failed"
    if ($script:fail -gt 0) { exit 1 }
}
finally {
    $ErrorActionPreference = 'Continue'
    foreach ($a in 'plain', 'crashy', 'loop') { try { & $m stop --timeout 1s $a *> $null } catch {} }
    Strays | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
    Remove-Item -Recurse -Force $home_ -ErrorAction SilentlyContinue
}
exit 0
