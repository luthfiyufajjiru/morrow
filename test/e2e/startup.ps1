# Start-at-boot checks with the real scheduled task. Run elevated on a
# throwaway machine (CI runner):
#   .\test\e2e\startup.ps1 -Morrow <morrow.exe>
# It simulates a reboot by killing every app and relay, then runs the startup
# task, and checks the apps come back running as SYSTEM (so they survive
# logoff). It removes everything at the end and refuses to run if Morrow is
# already installed here.
param([Parameter(Mandatory)] [string] $Morrow)
$ErrorActionPreference = 'Stop'

if ($env:CI -ne 'true') { throw 'Refusing to run outside CI: this script installs a scheduled task and a system install.' }
$principal = [Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { throw 'Run elevated.' }
$data = Join-Path $env:ProgramData 'Morrow'
if (Test-Path $data) { throw "Refusing: $data already exists." }
& schtasks /Query /TN 'Morrow\Startup' *> $null
if ($LASTEXITCODE -eq 0) { throw 'Refusing: the Morrow\Startup task already exists.' }

$bin = Join-Path $env:ProgramFiles 'MorrowE2EStartup'
$m = Join-Path $bin 'morrow.exe'
$marker = '10009'
$env:MORROW_HOME = $null

function Status($app) { ((& $m status $app) | Select-String '^Status: (.*)').Matches[0].Groups[1].Value }
function PidOf($app) { [int]((& $m status $app) | Select-String '^PID: (\d+)').Matches[0].Groups[1].Value }
function Alive($procId) { [bool](Get-Process -Id $procId -ErrorAction SilentlyContinue) }
function OwnerOf($procId) { (Invoke-CimMethod -InputObject (Get-CimInstance Win32_Process -Filter "ProcessId = $procId") -MethodName GetOwner).User }
function PowerLoss {
    Get-CimInstance Win32_Process | Where-Object {
        ($_.Name -eq 'PING.EXE' -and $_.CommandLine -match $marker) -or ($_.ExecutablePath -eq $m -and $_.CommandLine -match '_relay')
    } | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
    Start-Sleep -Seconds 1
}
function RunBootTask {
    & schtasks /Run /TN 'Morrow\Startup' | Out-Null
    for ($i = 0; $i -lt 30; $i++) {
        Start-Sleep -Seconds 1
        if ((Status api) -eq 'running' -and (Status web) -eq 'running') { return }
    }
}
$script:pass = 0; $script:fail = 0
function Ok([string] $name, [bool] $cond) {
    if ($cond) { $script:pass++; Write-Host "PASS  $name" } else { $script:fail++; Write-Host "FAIL  $name" }
}

try {
    New-Item -ItemType Directory -Force -Path $bin | Out-Null
    Copy-Item $Morrow $m
    & $m init | Out-Null
    foreach ($a in 'api', 'web', 'manual') { & $m create $a ping -n $marker 127.0.0.1 | Out-Null; & $m start $a | Out-Null }
    & $m stop manual | Out-Null

    Write-Host 'install:'
    & $m startup | Out-Null
    & schtasks /Query /TN 'Morrow\Startup' *> $null
    Ok 'scheduled task installed' ($LASTEXITCODE -eq 0)
    Ok '--status lists the wanted apps only' ([bool]((& $m startup --status) | Select-String 'Apps started at boot: api, web'))

    Write-Host 'after a simulated reboot:'
    $a1 = PidOf api; PowerLoss
    Ok 'apps are really gone' (-not (Alive $a1))
    RunBootTask
    $a2 = PidOf api
    Ok "wanted apps are started by the task ($a1 -> $a2)" ((Status api) -eq 'running' -and (Status web) -eq 'running' -and (Alive $a2))
    Ok 'they run as SYSTEM (services session, survives logoff)' ((OwnerOf $a2) -eq 'SYSTEM')
    Ok 'an app the user stopped stays stopped' ((Status manual) -ne 'running')
    Ok 'Administrators can still manage SYSTEM-started apps' ($(& $m restart api | Out-Null; (Status api) -eq 'running'))

    Write-Host 'remove:'
    $a3 = PidOf api
    & $m startup --remove | Out-Null
    & schtasks /Query /TN 'Morrow\Startup' *> $null
    Ok 'task removed' ($LASTEXITCODE -ne 0)
    Ok 'running apps were not stopped' ((Alive $a3) -and (Status api) -eq 'running')

    Write-Host 'uninstall removes the task:'
    & $m startup | Out-Null
    & $m uninstall --yes | Out-Null
    & schtasks /Query /TN 'Morrow\Startup' *> $null
    Ok 'task gone after uninstall' ($LASTEXITCODE -ne 0)

    Write-Host "`nresult: $($script:pass) passed, $($script:fail) failed"
    if ($script:fail -gt 0) { exit 1 }
}
finally {
    $ErrorActionPreference = 'Continue'
    try { & schtasks /Delete /TN 'Morrow\Startup' /F *> $null } catch {}
    foreach ($a in 'api', 'web', 'manual') { try { & $m stop --timeout 1s $a *> $null } catch {} }
    PowerLoss
    Remove-Item -Recurse -Force $data, $bin -ErrorAction SilentlyContinue
}
exit 0
