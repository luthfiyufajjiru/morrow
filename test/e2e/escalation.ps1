# Permission matrix for a Windows system install: what a regular (non-admin)
# user can and cannot do once an Administrator has initialized Morrow.
#
#   .\test\e2e\escalation.ps1 -Morrow <morrow.exe> -DemoApi <demo-api.exe>
#
# Must run elevated. It creates a throwaway local user, %ProgramData%\Morrow
# and %ProgramFiles%\MorrowE2E, so it only runs where CI=true (CI runners and
# throwaway VMs) and refuses if the Morrow data directory already exists.
param(
    [Parameter(Mandatory)] [string] $Morrow,
    [Parameter(Mandatory)] [string] $DemoApi
)
$ErrorActionPreference = 'Stop'

if ($env:CI -ne 'true') { throw 'Refusing to run outside CI: this script creates a local user account.' }
$principal = [Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { throw 'Run elevated.' }
$data = Join-Path $env:ProgramData 'Morrow'
if (Test-Path $data) { throw "Refusing: $data already exists." }

$bin = Join-Path $env:ProgramFiles 'MorrowE2E'
$m = Join-Path $bin 'morrow.exe'
$api = Join-Path $bin 'api'
$user = 'morrowe2e'
# A second account with a grant but outside the deployers group.
$outsider = 'morrowe2eout'
$secret = 'e2e-' + [guid]::NewGuid().ToString('N')
# The value submitted through the inbox; it must never be readable either.
$newSecret = 'e2einbox' + [guid]::NewGuid().ToString('N')
$plainPw = 'Mx!' + [guid]::NewGuid().ToString('N')
$securePw = ConvertTo-SecureString $plainPw -AsPlainText -Force
$deployGroup = 'Morrow Deployers'

function Cleanup {
    $ErrorActionPreference = 'Continue'
    try { & $m stop api *> $null } catch {}
    Get-Process demo-api -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    try { & net user $user /delete *> $null } catch {}
    try { & net user $outsider /delete *> $null } catch {}
    try { & net localgroup $deployGroup /delete *> $null } catch {}
    Remove-Item -Recurse -Force $data, $bin -ErrorAction SilentlyContinue
}

try {
    # Binaries under Program Files, which only Administrators can modify, like a real install.
    New-Item -ItemType Directory -Force -Path $api | Out-Null
    Copy-Item $Morrow $m
    Copy-Item $DemoApi (Join-Path $bin 'demo-api.exe')
    Set-Content -Path (Join-Path $api 'config.json') -Value '{}'
    $env:MORROW_HOME = $null; $env:MORROW_KEY_PATH = $null

    & $m init --enable-inbox --inbox-hook=false | Out-Null
    & $m create -w $api api (Join-Path $bin 'demo-api.exe') | Out-Null
    & $m create -w $api idle (Join-Path $bin 'demo-api.exe') | Out-Null
    & $m set-env api "API_TOKEN:s=$secret" PLAIN=visible | Out-Null
    & $m start api | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Administrator setup failed.' }
    Start-Sleep -Seconds 1

    # /y answers net's interactive prompt for passwords longer than 14 characters.
    & net user $user $plainPw /add /y | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Could not create the test user.' }
    $groups = (& net user $user) -join "`n"
    if ($groups -match 'Administrators') { throw "$user must not be an Administrator." }
    & net user $outsider $plainPw /add /y | Out-Null
    & net localgroup $deployGroup $user /add | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Could not add the test user to the deployers group.' }
    & $m grant $user --app api --vars API_TOKEN | Out-Null
    & $m grant $outsider --app api --vars API_TOKEN | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Could not grant API_TOKEN.' }

    Write-Host "ACLs after elevated 'morrow init':"
    foreach ($p in @($data, (Join-Path $data '.morrow.key'), (Join-Path $data 'morrow.db'), (Join-Path $data 'logs'))) {
        Write-Host "  $p"; (& icacls $p) | Select-Object -Skip 1 | Where-Object { $_.Trim() -and $_ -notmatch 'Successfully' } | ForEach-Object { Write-Host "     $($_.Trim())" }
    }

    # Runs a PowerShell command as the test user; returns exit code and combined output.
    function Invoke-AsUser([string] $command, [string] $as = $user) {
        $psi = New-Object System.Diagnostics.ProcessStartInfo
        $psi.FileName = 'powershell.exe'
        $encoded = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes("`$ErrorActionPreference='Stop'; `$m='$m'; $command"))
        $psi.Arguments = "-NoProfile -NonInteractive -EncodedCommand $encoded"
        $psi.UserName = $as
        $psi.Domain = $env:COMPUTERNAME
        $psi.Password = $securePw
        $psi.LoadUserProfile = $true
        $psi.UseShellExecute = $false
        $psi.RedirectStandardOutput = $true
        $psi.RedirectStandardError = $true
        $psi.WorkingDirectory = $bin
        $proc = [Diagnostics.Process]::Start($psi)
        $stdout = $proc.StandardOutput.ReadToEndAsync(); $stderr = $proc.StandardError.ReadToEndAsync()
        $proc.WaitForExit()
        return @{ Code = $proc.ExitCode; Out = ($stdout.Result + $stderr.Result) }
    }

    $script:pass = 0; $script:fail = 0
    # Check <ok|deny> <name> <command> [regex the output must match]; the secret must never appear.
    function Check([string] $expect, [string] $name, [string] $command, [string] $pattern = '', [string] $as = $user) {
        $r = Invoke-AsUser $command $as
        $ok = -not ($r.Out.Contains($secret) -or $r.Out.Contains($newSecret))
        if ($expect -eq 'ok') { $ok = $ok -and $r.Code -eq 0 } else { $ok = $ok -and $r.Code -ne 0 }
        if ($pattern) { $ok = $ok -and ($r.Out -match $pattern) }
        if ($ok) { $script:pass++; $tag = 'PASS' } else { $script:fail++; $tag = 'FAIL' }
        $last = (($r.Out -split "`r?`n") | Where-Object { $_.Trim() } | Select-Object -Last 1)
        if ($last.Length -gt 100) { $last = $last.Substring(0, 100) }
        Write-Host ('{0}  {1,-4} {2,-40} {3}' -f $tag, $expect, $name, $last)
    }
    $hint = 'Administrator terminal'
    $exit = '; exit $LASTEXITCODE'
    $key = Join-Path $data '.morrow.key'
    $db = Join-Path $data 'morrow.db'

    Write-Host "`nas $user (not an Administrator):"
    Check ok   'list'                              "& `$m list$exit" 'running'
    Check ok   'status'                            "& `$m status api$exit" 'running'
    Check ok   'detail masks secret'               "& `$m detail api$exit" 'API_TOKEN=\*\*\*\*'
    Check ok   'detail --json masks secret'        "& `$m detail api --json$exit" '"API_TOKEN": "\*\*\*\*"'
    Check ok   'list-env masks secret'             "& `$m list-env api$exit" 'API_TOKEN=\*\*\*\*'
    Check ok   'get-env unsecured'                 "& `$m get-env api PLAIN$exit" 'PLAIN=visible'
    Check deny 'get-env secured'                   "& `$m get-env api API_TOKEN$exit" $hint
    Check deny 'logs'                              "& `$m logs api -n 2$exit" $hint
    Check deny 'create'                            "& `$m create x cmd.exe$exit" $hint
    Check deny 'set-env (overwrite secret)'        "& `$m set-env api API_TOKEN=attacker$exit" $hint
    Check deny 'del-env'                           "& `$m del-env api PLAIN$exit" $hint
    Check deny 'update (swap executable)'          "& `$m update api cmd.exe$exit" $hint
    Check deny 'stop'                              "& `$m stop api$exit"
    Check deny 'restart'                           "& `$m restart api$exit"
    Check deny 'start (stopped app)'               "& `$m start idle$exit" $hint
    Check deny 'delete -f'                         "& `$m delete api -f$exit"
    Check deny 'init'                              "& `$m init$exit" $hint
    Check deny 'init --force (wipe secrets)'       "& `$m init --force$exit" $hint
    Check deny 'read key file'                     "Get-Content -Raw '$key'"
    Check deny 'overwrite database'                "Set-Content -Path '$db' -Value x"
    Check deny 'plant file next to database'       "New-Item -ItemType File -Path '$db-journal'"
    Check deny 'write into logs dir'               "New-Item -ItemType File -Path '$(Join-Path $data 'logs\x.log')'"
    Check deny 'replace morrow binary'             "Copy-Item C:\Windows\System32\whoami.exe '$m' -Force"
    Check deny 'own key via MORROW_KEY_PATH'       "`$k = Join-Path `$env:TEMP 'k'; [IO.File]::WriteAllBytes(`$k, [byte[]](1..32)); `$env:MORROW_KEY_PATH = `$k; & `$m get-env api API_TOKEN$exit" 'cannot decrypt'
    Check deny 'copied database, no key'           "`$h = Join-Path `$env:TEMP 'mh'; New-Item -ItemType Directory -Force `$h | Out-Null; Copy-Item '$db' `$h; `$env:MORROW_HOME = `$h; & `$m get-env api API_TOKEN$exit" 'cannot decrypt'
    Check ok   'own per-user install'              "`$h = Join-Path `$env:TEMP ('u' + [guid]::NewGuid()); `$env:MORROW_HOME = `$h; & `$m init | Out-Null; & `$m list$exit" 'No applications'

    $untracked = Get-CimInstance Win32_Process -Filter "Name = 'demo-api.exe'" | Where-Object {
        (Invoke-CimMethod -InputObject $_ -MethodName GetOwner).User -eq $user }
    if ($untracked) { $script:fail++; Write-Host "FAIL  deny ...no untracked process left by start: $($untracked.ProcessId)" }
    else { $script:pass++; Write-Host 'PASS  deny ...no untracked process left by start' }

    Write-Host "`nas Administrator afterwards (system state must be unchanged):"
    $status = (& $m status api) -join ' '
    $sec = (& $m get-env api API_TOKEN) -join ' '
    $plain = (& $m get-env api PLAIN) -join ' '
    $apps = ((& $m list) | Select-String '^\| (api|idle) ').Count
    if ($status -match 'running' -and $sec.Contains($secret) -and $plain -match 'visible' -and $apps -eq 2) {
        $script:pass++; Write-Host 'PASS  api running, secret and variables intact, apps intact'
    } else {
        $script:fail++; Write-Host "FAIL  system state changed: $status | apps=$apps"
    }

    # --- Write-only secrets through the inbox ---------------------------------
    $inbox = Join-Path $data 'inbox'
    $inboxKey = Join-Path $data '.morrow-inbox.key'
    Write-Host "`ninbox, as $user (member of '$deployGroup', granted API_TOKEN of api):"
    Check ok   'submit granted secret (from own env)'  "`$env:API_TOKEN = '$newSecret'; & `$m submit-env api API_TOKEN --restart$exit" 'Submitted 1'
    Check deny 'list the inbox'                        "Get-ChildItem -Force '$inbox'"
    Check deny 'read inbox private key'                "Get-Content -Raw '$inboxKey'"
    Check deny 'submit a variable not granted'         "& `$m submit-env api DB_PASS=abcdefgh12345678$exit" 'no grant'
    Check deny 'submit a denied name'                  "& `$m submit-env api PATHEXT=.EXE$exit" 'cannot be granted'
    Check deny 'submit a value outside the format'     "& `$m submit-env api 'API_TOKEN=`$(whoami)'$exit" 'token format'
    Check deny 'submit for another app'                "& `$m submit-env idle API_TOKEN=abcdefgh12345678$exit" 'no grant'
    Check deny 'apply as non-admin'                    "& `$m apply$exit" $hint
    Check ok   'plant a junk file (apply must reject)' "[IO.File]::WriteAllText('$inbox\0123456789abcdef0123456789abcdef.sealed', 'junk')"
    Check deny 'granted non-member cannot write inbox' "`$env:API_TOKEN = 'abcdefgh12345678'; & `$m submit-env api API_TOKEN$exit" $deployGroup $outsider

    Write-Host "`nas Administrator: morrow apply"
    $applied = (& $m apply) -join ' '
    Write-Host "  $applied"
    $sec = (& $m get-env api API_TOKEN) -join ' '
    $left = @(Get-ChildItem -Force $inbox).Count
    if ($applied -match 'Applied 1, rejected 1' -and $sec.Contains($newSecret) -and ((& $m status api) -join ' ') -match 'running' -and $left -eq 0) {
        $script:pass++; Write-Host 'PASS  submission applied, junk rejected, app restarted, inbox emptied'
    } else {
        $script:fail++; Write-Host "FAIL  apply result unexpected (files left: $left)"; & $m inbox
    }

    Write-Host "`nas $user after apply:"
    Check deny 'read the applied secret'               "& `$m get-env api API_TOKEN$exit" $hint
    Check ok   "see outcomes in 'morrow inbox'"        "& `$m inbox$exit" 'applied'
    Check ok   'outcomes show rejections, no values'   "& `$m inbox$exit" 'rejected'


    # --- Uninstall -------------------------------------------------------------
    function PassIf([string] $name, [bool] $cond) {
        if ($cond) { $script:pass++; Write-Host "PASS  $name" } else { $script:fail++; Write-Host "FAIL  $name" }
    }
    function DemoRunning { [bool](Get-Process demo-api -ErrorAction SilentlyContinue) }
    function GroupExists { & net localgroup $deployGroup *> $null; $LASTEXITCODE -eq 0 }
    $backup = Join-Path $env:TEMP 'morrow-e2e-key.bak'
    Remove-Item -Force $backup -ErrorAction SilentlyContinue

    Write-Host "`nuninstall, as ${user}:"
    Check deny 'uninstall'                         "& `$m uninstall --yes$exit" 'requires elevation'
    Check deny 'uninstall --purge'                 "& `$m uninstall --purge --yes$exit" 'requires elevation'

    Write-Host "`nuninstall, as Administrator:"
    & $m uninstall --dry-run | Out-Null
    PassIf '--dry-run changes nothing' ((DemoRunning) -and (Test-Path $inbox) -and (Test-Path (Join-Path $data '.morrow.key')) -and (GroupExists))
    'n' | & $m uninstall | Out-Null
    PassIf "answering 'n' changes nothing" ((DemoRunning) -and (Test-Path $inbox))
    & $m uninstall --yes | Out-Null
    PassIf 'uninstall stops apps, removes inbox and group' ((-not (DemoRunning)) -and -not (Test-Path $inbox) -and -not (Test-Path $inboxKey) -and -not (Test-Path (Join-Path $data 'inbox.pub')) -and -not (GroupExists))
    PassIf '...and keeps database, key and logs' ((Test-Path (Join-Path $data 'morrow.db')) -and (Test-Path (Join-Path $data '.morrow.key')) -and (Test-Path (Join-Path $data 'logs')))
    & $m init | Out-Null
    PassIf 'reinstall resumes with apps and secrets' ((((& $m list) -join "`n") -match '\| api ') -and ((& $m get-env api API_TOKEN) -join ' ').Contains($newSecret))
    & $m start api | Out-Null
    Start-Sleep -Seconds 1
    'yes' | & $m uninstall --purge | Out-Null
    PassIf "purge refuses anything but the word 'purge'" ((Test-Path (Join-Path $data '.morrow.key')) -and (DemoRunning))
    & $m uninstall --purge --yes --backup-key $backup | Out-Null
    PassIf 'purge removes all data, stops apps' ((-not (Test-Path $data)) -and -not (DemoRunning))
    PassIf '...backs up the key, keeps the binary' ((Test-Path $backup) -and (Get-Item $backup).Length -eq 32 -and (Test-Path $m))
    Remove-Item -Force $backup -ErrorAction SilentlyContinue

    Write-Host "`nresult: $($script:pass) passed, $($script:fail) failed"
    if ($script:fail -gt 0) { exit 1 }
}
finally {
    Cleanup
}
