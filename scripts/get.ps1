#Requires -Version 5.1
<#
.SYNOPSIS
  aidev-clis remote install (Windows): download a release archive from GitHub,
  verify its sha256, and run the bundled installer in prebuilt mode.

  irm https://raw.githubusercontent.com/no-today/aidev-clis/main/scripts/get.ps1 | iex

  $env:AIDEV_VERSION pins a tag (e.g. v2026.7.3-pre); default is the latest release.
#>
$ErrorActionPreference = 'Stop'

$RepoSlug = 'no-today/aidev-clis'

$arch = switch ((Get-CimInstance Win32_Processor).Architecture) {
    9 { 'amd64' }   # x64
    12 { 'arm64' }  # ARM64
    default { throw "unsupported arch (supported: amd64 arm64)" }
}

# Resolve the tag: /releases/latest redirects to /releases/tag/<tag>.
$tag = $env:AIDEV_VERSION
if (-not $tag) {
    $resp = Invoke-WebRequest -Uri "https://github.com/$RepoSlug/releases/latest" -Method Head -MaximumRedirection 5 -UseBasicParsing
    # Windows PowerShell 5.1 (HttpWebResponse) vs PowerShell 7 (HttpResponseMessage)
    if ($resp.BaseResponse.PSObject.Properties['ResponseUri']) {
        $finalUri = $resp.BaseResponse.ResponseUri
    } else {
        $finalUri = $resp.BaseResponse.RequestMessage.RequestUri
    }
    $tag = $finalUri.Segments[-1].TrimEnd('/')
    if ($tag -notmatch '^v') {
        # /releases/latest excludes prereleases; when only prereleases exist it
        # doesn't redirect to a tag. Fall back to the newest release of any kind.
        $rel = Invoke-RestMethod -Uri "https://api.github.com/repos/$RepoSlug/releases?per_page=1" -UseBasicParsing
        if ($rel -and $rel[0].tag_name -match '^v') { $tag = $rel[0].tag_name }
        else { throw "cannot resolve any release; is there a release yet?" }
    }
}
$version = $tag.TrimStart('v')

$asset = "aidev-clis_${version}_windows_${arch}.zip"
$base  = "https://github.com/$RepoSlug/releases/download/$tag"

$tmp = Join-Path ([IO.Path]::GetTempPath()) "aidev-clis-get-$([IO.Path]::GetRandomFileName())"
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    Write-Host ">> downloading $asset ($tag)"
    Invoke-WebRequest -Uri "$base/$asset" -OutFile (Join-Path $tmp $asset) -UseBasicParsing
    Invoke-WebRequest -Uri "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt') -UseBasicParsing

    Write-Host ">> verifying sha256"
    $want = (Get-Content (Join-Path $tmp 'checksums.txt') | Where-Object { $_ -match [regex]::Escape($asset) }) -split '\s+' | Select-Object -First 1
    if (-not $want) { throw "checksum for $asset not found in checksums.txt" }
    $got = (Get-FileHash -Algorithm SHA256 (Join-Path $tmp $asset)).Hash.ToLower()
    if ($got -ne $want.ToLower()) { throw "sha256 mismatch for $asset (want $want, got $got)" }

    Expand-Archive -Path (Join-Path $tmp $asset) -DestinationPath $tmp
    $installer = Get-ChildItem -Path $tmp -Recurse -Filter install.ps1 |
        Where-Object { $_.Directory.Name -eq 'scripts' } | Select-Object -First 1
    if (-not $installer) { throw "install.ps1 not found in archive" }
    & $installer.FullName
} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
