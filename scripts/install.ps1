# install.ps1 — install the `rp` CLI on native Windows (PowerShell) without Go.
#
# Detects your arch, downloads the matching prebuilt rp.exe from the latest GitHub Release,
# installs it under %LOCALAPPDATA%\Programs\rp with an rc.exe copy (the pre-rename command name,
# kept working), and puts that dir on your user PATH. Idempotent: re-run to upgrade. A pre-rename
# install in %LOCALAPPDATA%\Programs\rc is removed and its PATH entry replaced.
#
#   irm https://raw.githubusercontent.com/replypen/replypen-cli/main/scripts/install.ps1 | iex
#
# Knobs (env vars):
#   RC_VERSION       install a specific version instead of latest, e.g. $env:RC_VERSION = "v0.5.1"
#   RC_INSTALL_DIR   install into this dir instead of %LOCALAPPDATA%\Programs\rp
#
# (On WSL use scripts/install.sh instead — WSL is Linux.)

$ErrorActionPreference = "Stop"
$repo = "replypen/replypen-cli"

# --- detect arch -------------------------------------------------------------
$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
  "AMD64" { "amd64" }
  "ARM64" { "arm64" }
  default { throw "unsupported arch '$($env:PROCESSOR_ARCHITECTURE)' (need AMD64 or ARM64)" }
}

# --- resolve version ---------------------------------------------------------
if ($env:RC_VERSION) {
  $tag = if ($env:RC_VERSION.StartsWith("v")) { $env:RC_VERSION } else { "v$($env:RC_VERSION)" }
} else {
  Write-Host "==> resolving latest release" -ForegroundColor Cyan
  $tag = (Invoke-RestMethod "https://api.github.com/repos/$repo/releases/latest").tag_name
  if (-not $tag) { throw "could not resolve the latest release tag from the GitHub API" }
}
$version = $tag.TrimStart("v")

$asset = "rp_${version}_windows_${arch}.zip"
$url   = "https://github.com/$repo/releases/download/$tag/$asset"

# --- install dir -------------------------------------------------------------
$bindir = if ($env:RC_INSTALL_DIR) { $env:RC_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "Programs\rp" }
$legacyDir = Join-Path $env:LOCALAPPDATA "Programs\rc"
New-Item -ItemType Directory -Force -Path $bindir | Out-Null
$target = Join-Path $bindir "rp.exe"
$alias = Join-Path $bindir "rc.exe"

function Test-SamePath($a, $b) {
  (Test-Path $a) -and (Test-Path $b) -and ((Resolve-Path $a).Path -eq (Resolve-Path $b).Path)
}

$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
$migrateLegacy = (-not $env:RC_INSTALL_DIR) -and (Test-Path (Join-Path $legacyDir "rc.exe"))

# Never create a second, shadowed copy: any rp.exe/rc.exe PATH selects must be the one in $bindir (or
# the pre-rename install this run replaces).
foreach ($cmd in "rp.exe", "rc.exe") {
  $selectedBefore = (Get-Command $cmd -ErrorAction SilentlyContinue).Source
  if (-not $selectedBefore) { continue }
  if ($migrateLegacy -and ((Split-Path -Parent $selectedBefore).TrimEnd("\") -eq $legacyDir)) { continue }
  if (-not (Test-SamePath $selectedBefore (Join-Path $bindir $cmd))) {
    throw "PATH already selects $selectedBefore; refusing to create a second install at $bindir (run '$selectedBefore self doctor')"
  }
}

# --- download + extract ------------------------------------------------------
$tmp = Join-Path $env:TEMP ("rp-" + [guid]::NewGuid())
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
try {
  Write-Host "==> downloading $asset ($tag)" -ForegroundColor Cyan
  $zip = Join-Path $tmp "rp.zip"
  Invoke-WebRequest -Uri $url -OutFile $zip
  $checksums = Invoke-WebRequest -Uri "https://github.com/$repo/releases/download/$tag/checksums.txt"
  $line = ($checksums.Content -split "`n" | Where-Object { $_ -match "\s+$([regex]::Escape($asset))\s*$" } | Select-Object -First 1)
  if (-not $line) { throw "checksums.txt has no entry for $asset" }
  $want = ($line.Trim() -split "\s+")[0].ToLowerInvariant()
  $got = (Get-FileHash -Algorithm SHA256 -Path $zip).Hash.ToLowerInvariant()
  if ($got -ne $want) { throw "checksum mismatch for $asset — refusing to install" }
  Expand-Archive -Path $zip -DestinationPath $tmp -Force

  # The download is verified: only now retire the pre-rename install (Programs\rc\rc.exe + its PATH entry).
  if ($migrateLegacy) {
    Write-Host "==> replacing the pre-rename install in $legacyDir" -ForegroundColor Cyan
    Remove-Item -Force (Join-Path $legacyDir "rc.exe*"), (Join-Path $legacyDir "rp.exe*") -ErrorAction SilentlyContinue
    if (-not (Get-ChildItem $legacyDir -ErrorAction SilentlyContinue)) { Remove-Item -Force $legacyDir -ErrorAction SilentlyContinue }
    $userPath = (($userPath -split ";") | Where-Object { $_ -and ($_.TrimEnd("\") -ne $legacyDir) }) -join ";"
    [Environment]::SetEnvironmentVariable("Path", $userPath, "User")
    $env:Path = (($env:Path -split ";") | Where-Object { $_ -and ($_.TrimEnd("\") -ne $legacyDir) }) -join ";"
  }
  Copy-Item -Path (Join-Path $tmp "rp.exe") -Destination $target -Force
  # Windows has no unprivileged symlinks: the alias is a byte-identical copy (rp self update refreshes it).
  Copy-Item -Path (Join-Path $tmp "rp.exe") -Destination $alias -Force
} finally {
  Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

Write-Host "==> installed rp $version -> $target (rc.exe alias)" -ForegroundColor Cyan

# --- add to user PATH --------------------------------------------------------
if (($userPath -split ";") -notcontains $bindir) {
  [Environment]::SetEnvironmentVariable("Path", "$userPath;$bindir", "User")
  Write-Host "==> added $bindir to your user PATH — open a new terminal to pick it up" -ForegroundColor Yellow
}
$env:Path = "$env:Path;$bindir"
& $target --version
foreach ($cmd in "rp.exe", "rc.exe") {
  $selected = (Get-Command $cmd -ErrorAction SilentlyContinue).Source
  if ($selected -and -not (Test-SamePath $selected (Join-Path $bindir $cmd))) {
    throw "installed $target, but PATH still selects $selected for $cmd; run '$target self doctor' and remove the shadowing install"
  }
}
