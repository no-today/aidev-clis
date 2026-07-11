#Requires -Version 5.1
<#
.SYNOPSIS
  aidev-clis uninstall (Windows): remove binaries, skills, completions, and (on
  a full uninstall) the PATH entry + $PROFILE wiring. Native equivalent of
  scripts/uninstall.sh.

.PARAMETER Clis
  CLIs to remove (positional). Empty = all (also removes aidev + PATH + profile).

.PARAMETER InstallDir
  Where the .exe files live. Default: %LOCALAPPDATA%\Programs\aidev-clis.

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File scripts\uninstall.ps1
#>
[CmdletBinding()]
param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]] $Clis,
    [string]   $InstallDir = (Join-Path $env:LOCALAPPDATA 'Programs\aidev-clis')
)

$ErrorActionPreference = 'Stop'

$AllClis = @('apicli', 'dbcli', 'jcli', 'logcli', 'tcli')

# Match install.ps1: agents (Node) use %USERPROFILE%, which can differ from
# PowerShell's $HOME on domain-joined machines.
$UserHome = if ($env:USERPROFILE) { $env:USERPROFILE } else { $HOME }
$ClaudeSkillsDir = Join-Path $UserHome '.claude\skills'
$CodexSkillsDir  = Join-Path $UserHome '.codex\skills'
$SkillDirs       = @($ClaudeSkillsDir, $CodexSkillsDir)
$CompletionsDir  = Join-Path $InstallDir 'completions'
$ProfileMarkerStart = '# >>> aidev-clis completions >>>'
$ProfileMarkerEnd   = '# <<< aidev-clis completions <<<'

$FullUninstall = -not ($PSBoundParameters.ContainsKey('Clis') -and $Clis.Count -gt 0)

if ($FullUninstall) {
    $selected = $AllClis
} else {
    foreach ($c in $Clis) {
        if ($AllClis -notcontains $c) {
            Write-Error "unknown cli '$c' (valid: $($AllClis -join ' '))"
        }
    }
    $selected = $Clis
}

# Remove a path if present; silent if already gone. Supports -WhatIf.
function Remove-ItemIfPresent {
    [CmdletBinding(SupportsShouldProcess = $true)]
    param([string] $Path)
    if ($Path -and (Test-Path $Path)) { Remove-Item -Recurse -Force $Path }
}

function Uninstall-Cli {
    param([string] $Cli, [string] $SkillName)
    Write-Host ">> removing $Cli"
    Remove-ItemIfPresent (Join-Path $InstallDir "$Cli.exe")
    Remove-ItemIfPresent (Join-Path $CompletionsDir "$Cli.ps1")
    if ($SkillName) {
        foreach ($sdir in $SkillDirs) {
            Remove-ItemIfPresent (Join-Path $sdir $SkillName)
        }
    }
}

foreach ($cli in $selected) {
    Uninstall-Cli -Cli $cli -SkillName "aidev-$cli"
}

if ($FullUninstall) {
    Write-Host ">> removing aidev from $InstallDir"
    Uninstall-Cli -Cli 'aidev' -SkillName 'use-aidev'

    # Strip the guarded completion block from the profile install.ps1 wrote.
    $profilePath = $PROFILE.CurrentUserAllHosts
    if (Test-Path $profilePath) {
        $existing = Get-Content -Raw $profilePath
        $pattern  = [regex]::Escape($ProfileMarkerStart) + '.*?' + [regex]::Escape($ProfileMarkerEnd)
        $stripped = [regex]::Replace($existing, $pattern, '', 'Singleline').TrimEnd("`r", "`n")
        if ($stripped) { Set-Content -Path $profilePath -Value ($stripped + "`r`n") -Encoding utf8 }
        else { Remove-Item -Force $profilePath }
        Write-Host ">> removed completion wiring from $profilePath"
    }

    # Remove InstallDir from the User PATH.
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if ($userPath) {
        $parts = $userPath -split ';' | Where-Object { $_ -and $_ -ne $InstallDir }
        [Environment]::SetEnvironmentVariable('Path', ($parts -join ';'), 'User')
        Write-Host ">> removed $InstallDir from User PATH"
    }
    $env:Path = (($env:Path -split ';' | Where-Object { $_ -and $_ -ne $InstallDir }) -join ';')

    # Drop the (now empty) install dir if nothing else remains.
    if ((Test-Path $InstallDir) -and -not (Get-ChildItem -Force $InstallDir -Recurse | Where-Object { -not $_.PSIsContainer })) {
        Remove-ItemIfPresent $InstallDir
    }
}

Write-Host "OK uninstalled: $($selected -join ' ')"
