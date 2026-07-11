#Requires -Version 5.1
<#
.SYNOPSIS
  aidev-clis install (Windows): build CLIs, add to PATH, copy skills, install
  PowerShell completions. Native equivalent of scripts/install.sh (user scope,
  no admin required).

.PARAMETER Clis
  CLIs to install (positional). Empty = all. Valid: apicli dbcli jcli logcli tcli.

.PARAMETER InstallDir
  Where the .exe files go. Default: %LOCALAPPDATA%\Programs\aidev-clis.

.PARAMETER NoPath        Skip adding InstallDir to the User PATH.
.PARAMETER NoCompletion  Skip PowerShell completion generation + $PROFILE edit.
.PARAMETER NoSkills      Skip copying skills into the agent skill dirs.

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File scripts\install.ps1
.EXAMPLE
  powershell -ExecutionPolicy Bypass -File scripts\install.ps1 dbcli logcli
#>
[CmdletBinding()]
param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]] $Clis,
    [string]   $InstallDir = (Join-Path $env:LOCALAPPDATA 'Programs\aidev-clis'),
    [switch]   $NoPath,
    [switch]   $NoCompletion,
    [switch]   $NoSkills
)

$ErrorActionPreference = 'Stop'

$RepoRoot  = Split-Path -Parent $PSScriptRoot
$AllClis   = @('apicli', 'dbcli', 'jcli', 'logcli', 'tcli')
# Skills shipped by the old monolithic `aidev` repo that this repo supersedes.
# Removed on every install so a fresh install fully migrates an old setup.
$StaleSkills = if ($null -ne $env:AIDEV_STALE_SKILLS) { $env:AIDEV_STALE_SKILLS -split '\s+' | Where-Object { $_ } } else { @('aidev') }

# Skills go where the agents (Node apps: os.homedir() == %USERPROFILE%) read
# them. PowerShell's $HOME is HOMEDRIVE+HOMEPATH, which can differ from
# USERPROFILE on domain-joined machines, so prefer USERPROFILE.
$UserHome = if ($env:USERPROFILE) { $env:USERPROFILE } else { $HOME }
$ClaudeSkillsDir = Join-Path $UserHome '.claude\skills'
$CodexSkillsDir  = Join-Path $UserHome '.codex\skills'
$SkillDirs       = @($ClaudeSkillsDir, $CodexSkillsDir)
$CompletionsDir  = Join-Path $InstallDir 'completions'
# Release archives ship prebuilt binaries in bin\ and no go.mod; a source
# checkout has go.mod. That marker picks the mode: prebuilt installs skip
# `go build` and everything else (skills, completions, PATH) is identical.
$Prebuilt = -not (Test-Path (Join-Path $RepoRoot 'go.mod'))
$ProfileMarkerStart = '# >>> aidev-clis completions >>>'
$ProfileMarkerEnd   = '# <<< aidev-clis completions <<<'

# A full install (no explicit CLI args) also installs the `aidev` aggregator.
$FullInstall = -not ($PSBoundParameters.ContainsKey('Clis') -and $Clis.Count -gt 0)

# Validate requested CLIs; no args selects all.
if ($FullInstall) {
    $selected = $AllClis
} else {
    foreach ($c in $Clis) {
        if ($AllClis -notcontains $c) {
            Write-Error "unknown cli '$c' (valid: $($AllClis -join ' '))"
        }
    }
    $selected = $Clis
}

New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null

# --- stale-skill removal -----------------------------------------------------
foreach ($sdir in $SkillDirs) {
    foreach ($stale in $StaleSkills) {
        $p = Join-Path $sdir $stale
        if (Test-Path $p) {
            Write-Host ">> removing stale skill $stale from $sdir"
            Remove-Item -Recurse -Force $p
        }
    }
}

# Example config(s) copied into each installed skill dir, so an agent using the
# skill (which has no repo checkout) can read a full, commented sample. examples/
# stays the single source; the release archive ships it (see .goreleaser.yaml).
function Get-SkillExample([string] $SkillName) {
    switch ($SkillName) {
        'aidev-apicli' { @('apicli.yaml', 'actors.yaml') }
        'aidev-dbcli'  { @('dbcli.yaml') }
        'aidev-jcli'   { @('jcli.yaml') }
        'aidev-logcli' { @('logcli.yaml') }
        'aidev-tcli'   { @('tcli-case.yaml', 'tcli-case-minimal.yaml') }
        'use-aidev'    { @('.aidev.yaml') }
        default        { @() }
    }
}

# Build one CLI, install its .exe, skill, and completion.
function Install-Cli {
    param([string] $Cli, [string] $SkillName)

    $exe = Join-Path $InstallDir "$Cli.exe"
    if ($Prebuilt) {
        Write-Host ">> installing prebuilt $Cli to $InstallDir"
        New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
        Copy-Item -Force (Join-Path $RepoRoot "bin\$Cli.exe") $exe
    } else {
        Write-Host ">> building + installing $Cli to $InstallDir"
        # Build straight into InstallDir. The repo bin\ dir is gitignored (absent
        # on a fresh clone) and `go build -o` will not create a missing parent dir.
        Push-Location $RepoRoot
        try {
            & go build -o $exe ".\cmd\$Cli"
            if ($LASTEXITCODE -ne 0) { throw "go build failed for $Cli" }
        } finally {
            Pop-Location
        }
    }

    if (-not $NoSkills -and $SkillName) {
        foreach ($sdir in $SkillDirs) {
            Write-Host ">> installing skill $SkillName to $sdir"
            New-Item -ItemType Directory -Force -Path $sdir | Out-Null
            $dest = Join-Path $sdir $SkillName
            if (Test-Path $dest) { Remove-Item -Recurse -Force $dest }
            Copy-Item -Recurse -Force (Join-Path $RepoRoot "skills\$SkillName") $dest
            foreach ($ex in (Get-SkillExample $SkillName)) {
                $src = Join-Path $RepoRoot "examples\$ex"
                if (Test-Path $src) { Copy-Item -Force $src $dest }
            }
        }
    }

    if (-not $NoCompletion) {
        New-Item -ItemType Directory -Force -Path $CompletionsDir | Out-Null
        $comp = Join-Path $CompletionsDir "$Cli.ps1"
        & (Join-Path $InstallDir "$Cli.exe") completion powershell |
            Out-File -FilePath $comp -Encoding utf8
    }
}

foreach ($cli in $selected) {
    Install-Cli -Cli $cli -SkillName "aidev-$cli"
}

# aidev: the cross-CLI discovery aggregator (full install only).
if ($FullInstall) {
    Install-Cli -Cli 'aidev' -SkillName 'use-aidev'
}

# --- PATH --------------------------------------------------------------------
if (-not $NoPath) {
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    # @(...) forces an array: a one-entry User PATH (e.g. just WindowsApps, the
    # Windows default) otherwise collapses to a scalar string, and `+ $InstallDir`
    # below would string-concatenate instead of appending, corrupting PATH.
    $parts = @($userPath -split ';' | Where-Object { $_ })
    if ($parts -notcontains $InstallDir) {
        $newPath = (@($parts + $InstallDir) -join ';')
        # NOT setx: it silently truncates PATH at 1024 chars.
        # SetEnvironmentVariable broadcasts WM_SETTINGCHANGE so new shells pick
        # the change up. Trade-off: it writes REG_SZ, flattening any %VAR% form
        # in the existing User PATH - acceptable (rare in User PATH; still a
        # valid absolute path) and worth it for the broadcast.
        [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
        Write-Host ">> added $InstallDir to User PATH"
    } else {
        Write-Host ">> User PATH already contains $InstallDir"
    }
    # Make the CLIs usable in the current session too.
    if (($env:Path -split ';') -notcontains $InstallDir) {
        $env:Path = "$env:Path;$InstallDir"
    }
}

# --- completion wiring in $PROFILE ------------------------------------------
if (-not $NoCompletion) {
    # CurrentUserAllHosts (profile.ps1) loads in every host of this PowerShell
    # edition (console, VSCode, ISE), not just the current one. Note: the 5.1
    # and 7 editions have separate profiles - re-run under the other to wire it.
    $profilePath = $PROFILE.CurrentUserAllHosts
    $profileDir  = Split-Path -Parent $profilePath
    if ($profileDir -and -not (Test-Path $profileDir)) {
        New-Item -ItemType Directory -Force -Path $profileDir | Out-Null
    }

    $existing = if (Test-Path $profilePath) { Get-Content -Raw $profilePath } else { '' }

    # Strip any previous block (idempotent), then append a fresh one.
    $pattern = [regex]::Escape($ProfileMarkerStart) + '.*?' + [regex]::Escape($ProfileMarkerEnd)
    $stripped = [regex]::Replace($existing, $pattern, '', 'Singleline').TrimEnd("`r", "`n")

    $block = @"
$ProfileMarkerStart
Get-ChildItem -Path '$CompletionsDir' -Filter *.ps1 -ErrorAction SilentlyContinue | ForEach-Object { . `$_.FullName }
$ProfileMarkerEnd
"@

    $content = if ($stripped) { "$stripped`r`n`r`n$block`r`n" } else { "$block`r`n" }
    Set-Content -Path $profilePath -Value $content -Encoding utf8
    Write-Host ">> wired completions into $profilePath"
}

Write-Host ''
Write-Host "OK installed: $($selected -join ' ')"
Write-Host "  binaries: $InstallDir"
Write-Host "  skills:   $ClaudeSkillsDir and $CodexSkillsDir (aidev-<cli>)"
if ($FullInstall) {
    Write-Host "  + aidev (cross-CLI discovery) binary and the use-aidev skill"
}
if (-not $NoPath) {
    Write-Host "  PATH: open a NEW terminal to pick up the updated User PATH"
}
if (-not $NoCompletion) {
    Write-Host "  completions: restart PowerShell (or run: . `$PROFILE) to enable tab-completion"
}
