#Requires -Version 5.1
<#
.SYNOPSIS
  Native-Windows smoke test: install.ps1 -> assert install -> exercise the CLIs
  on real Windows (self-contained: no external DB/log/API services) -> assert
  uninstall reverts everything. Runnable in CI (windows-latest) or a Windows VM.

  Exits non-zero if any check fails. Uses an isolated AIDEV_CLIS_HOME and an
  InstallDir under the temp dir so it never disturbs a real setup - though it
  does touch the current user's PATH / $PROFILE / skill dirs (install.ps1's job)
  and reverts them via uninstall.ps1 at the end.

.PARAMETER RepoRoot
  Repo checkout root. Defaults to two levels up from this script (scripts\ci\).
#>
[CmdletBinding()]
param(
    [string] $RepoRoot = (Split-Path -Parent (Split-Path -Parent $PSScriptRoot))
)

$ErrorActionPreference = 'Stop'
$script:fails = 0

function Check {
    param([string] $Name, [scriptblock] $Test)
    try {
        if (& $Test) { Write-Host "  PASS  $Name" }
        else { Write-Host "  FAIL  $Name"; $script:fails++ }
    } catch {
        Write-Host "  FAIL  $Name -> $_"; $script:fails++
    }
}

# Run a CLI and parse its JSON envelope from stdout (diagnostics go to stderr).
function Invoke-Json {
    param([string] $Exe, [string[]] $CliArgs)
    $out = & $Exe @CliArgs 2>$null
    return ($out | Out-String | ConvertFrom-Json)
}

# --- isolated workspace -----------------------------------------------------
$work = Join-Path ([System.IO.Path]::GetTempPath()) ("aidev-ci-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force -Path $work | Out-Null
$env:AIDEV_CLIS_HOME = Join-Path $work 'home'
New-Item -ItemType Directory -Force -Path $env:AIDEV_CLIS_HOME | Out-Null
$InstallDir = Join-Path $work 'bin'
Write-Host "workspace: $work"
Write-Host "AIDEV_CLIS_HOME: $env:AIDEV_CLIS_HOME"

$allClis = @('apicli', 'dbcli', 'jcli', 'logcli', 'tcli', 'aidev')

try {
    # === install ============================================================
    Write-Host "`n== install.ps1 =="
    & (Join-Path $RepoRoot 'scripts\install.ps1') -InstallDir $InstallDir
    # Put InstallDir on this process's PATH so tcli can spawn dbcli by name.
    $env:Path = "$InstallDir;$env:Path"

    Write-Host "`n== assert install =="
    foreach ($c in $allClis) {
        Check "$c.exe installed" { Test-Path (Join-Path $InstallDir "$c.exe") }
    }
    Check "InstallDir on User PATH" {
        ([Environment]::GetEnvironmentVariable('Path', 'User') -split ';') -contains $InstallDir
    }
    Check "skill aidev-dbcli copied to USERPROFILE" {
        Test-Path (Join-Path $env:USERPROFILE '.claude\skills\aidev-dbcli')
    }
    Check "bundled example dbcli.yaml copied into the skill dir" {
        Test-Path (Join-Path $env:USERPROFILE '.claude\skills\aidev-dbcli\dbcli.yaml')
    }
    Check "use-aidev skill copied (full install)" {
        Test-Path (Join-Path $env:USERPROFILE '.claude\skills\use-aidev')
    }
    Check "completion script generated" {
        Test-Path (Join-Path $InstallDir 'completions\dbcli.ps1')
    }
    Check "profile wired with completion block" {
        $p = $PROFILE.CurrentUserAllHosts
        (Test-Path $p) -and ((Get-Content -Raw $p) -match 'aidev-clis completions')
    }
    Check "completion script parses" {
        $t = $null; $e = $null
        [System.Management.Automation.Language.Parser]::ParseFile(
            (Join-Path $InstallDir 'completions\dbcli.ps1'), [ref]$t, [ref]$e) | Out-Null
        -not $e
    }

    # === fixtures (self-contained) =========================================
    $dbPath  = Join-Path $work 'app.db'
    $pyFile  = Join-Path $work 'seed.py'
    @"
import sqlite3, sys
c = sqlite3.connect(sys.argv[1])
c.execute('CREATE TABLE t (id INTEGER, name TEXT)')
c.execute("INSERT INTO t VALUES (1,'alice')")
c.commit(); c.close()
"@ | Set-Content -Path $pyFile -Encoding ascii
    & python $pyFile $dbPath
    if ($LASTEXITCODE -ne 0) { throw "python failed to seed sqlite fixture" }

    $dsn = 'file:' + ($dbPath -replace '\\', '/')
    @"
default_target: local
targets:
  local: { adapter: sqlite, description: ci, dsn: "$dsn" }
"@ | Set-Content -Path (Join-Path $env:AIDEV_CLIS_HOME 'dbcli.yaml') -Encoding ascii

    $logPath = Join-Path $work 'app.log'
    "line one`r`nBOOM error here`r`nline three" | Set-Content -Path $logPath -Encoding ascii
    $logForYaml = $logPath -replace '\\', '/'
    @"
default_target: local
targets:
  local: { adapter: local-file, files: ["$logForYaml"] }
"@ | Set-Content -Path (Join-Path $env:AIDEV_CLIS_HOME 'logcli.yaml') -Encoding ascii

    $casePath = Join-Path $work 'case.yaml'
    @"
name: ci smoke
dbs: { main: local }
steps:
  # Query the stable seeded row by id: the earlier write-guard check may have
  # inserted a second row, so do NOT assert a whole-table count here.
  - name: seeded row readable
    db: { sql: "SELECT name FROM t WHERE id = 1", expect: [ "rows.0.0 == alice" ] }
"@ | Set-Content -Path $casePath -Encoding ascii

    $sceneDir = Join-Path $work 'scene'
    New-Item -ItemType Directory -Force -Path $sceneDir | Out-Null
    'scene: ci' | Set-Content -Path (Join-Path $sceneDir '.aidev.yaml') -Encoding ascii

    $dbcli  = Join-Path $InstallDir 'dbcli.exe'
    $logcli = Join-Path $InstallDir 'logcli.exe'
    $tcli   = Join-Path $InstallDir 'tcli.exe'
    $aidev  = Join-Path $InstallDir 'aidev.exe'

    # === runtime: the CLIs actually work on Windows ========================
    Write-Host "`n== runtime smoke =="
    foreach ($c in $allClis) {
        Check "$c --help exits 0" { & (Join-Path $InstallDir "$c.exe") --help *> $null; $LASTEXITCODE -eq 0 }
    }
    Check "dbcli targets lists 'local'" {
        (Invoke-Json $dbcli @('targets')).data.name -contains 'local'
    }
    Check "dbcli sqlite SELECT returns alice (Windows file URI)" {
        (Invoke-Json $dbcli @('sqlite', '--target', 'local', 'SELECT name FROM t WHERE id=1')).data.rows[0][0] -eq 'alice'
    }
    Check "dbcli write refused without --allow-write" {
        (Invoke-Json $dbcli @('sqlite', '--target', 'local', "INSERT INTO t VALUES (2,'bob')")).error.code -eq 'WRITE_NOT_ALLOWED'
    }
    Check "dbcli write permitted with --allow-write" {
        (Invoke-Json $dbcli @('sqlite', '--target', 'local', '--allow-write', "INSERT INTO t VALUES (2,'bob')")).data.affected -eq 1
    }
    Check "logcli local-file logs finds the BOOM line (CRLF file)" {
        $j = Invoke-Json $logcli @('local-file', '--target', 'local', 'logs', '--tail', '10')
        ($j.data.message -join "`n") -match 'BOOM error here'
    }
    Check "tcli validate accepts the example case" {
        (Invoke-Json $tcli @('validate', (Join-Path $RepoRoot 'examples\tcli-case.yaml'))).data.valid -eq $true
    }
    Check "tcli run PASS (spawns dbcli subprocess -> sqlite)" {
        (Invoke-Json $tcli @('run', $casePath)).data.verdict -eq 'PASS'
    }
    Push-Location $sceneDir
    try {
        Check "aidev discovery sees dbcli in the scene" {
            (Invoke-Json $aidev @()).data.tools -contains 'dbcli'
        }
    } finally { Pop-Location }

    # === uninstall + assert revert =========================================
    Write-Host "`n== uninstall.ps1 =="
    & (Join-Path $RepoRoot 'scripts\uninstall.ps1') -InstallDir $InstallDir

    Write-Host "`n== assert uninstall reverted =="
    Check "dbcli.exe removed" { -not (Test-Path $dbcli) }
    Check "InstallDir removed from User PATH" {
        ([Environment]::GetEnvironmentVariable('Path', 'User') -split ';') -notcontains $InstallDir
    }
    Check "skill aidev-dbcli removed" {
        -not (Test-Path (Join-Path $env:USERPROFILE '.claude\skills\aidev-dbcli'))
    }
    Check "profile completion block stripped" {
        $p = $PROFILE.CurrentUserAllHosts
        -not ((Test-Path $p) -and ((Get-Content -Raw $p) -match 'aidev-clis completions'))
    }
} finally {
    Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $work
}

Write-Host ''
if ($script:fails -gt 0) {
    Write-Host "SMOKE FAILED: $script:fails check(s) failed"
    exit 1
}
Write-Host 'SMOKE PASSED: all checks green'
