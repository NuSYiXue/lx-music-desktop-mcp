# Build lx-music-mcp and place the artifact into mcp\.
#
# Usage:
#   .\build.ps1                        # build for this machine (windows/amd64)
#   .\build.ps1 -Version 2.12.6-ai-1.0 # inject a version string
#   .\build.ps1 -All                   # cross-compile every supported platform
#
# Equivalent command under bash:
#   go build -trimpath -ldflags "-s -w -X main.version=dev" -o mcp/lx-music-mcp.exe ./cmd/lx-music-mcp
#
# NOTE: keep this file ASCII-only. Windows PowerShell 5.1 reads .ps1 files
# without a BOM as ANSI, which corrupts non-ASCII characters.

param(
    [string]$Version = "dev",
    [switch]$All
)

$ErrorActionPreference = "Stop"
$root = $PSScriptRoot
$outDir = Join-Path $root "mcp"

New-Item -ItemType Directory -Force -Path $outDir | Out-Null

# CGO_ENABLED=0 keeps the artifact a statically linked single file with no runtime dependency.
$env:CGO_ENABLED = "0"

function Build-One {
    param([string]$goos, [string]$goarch, [string]$name)

    $env:GOOS = $goos
    $env:GOARCH = $goarch
    $out = Join-Path $outDir $name

    Write-Host "building $goos/$goarch -> mcp/$name"
    go build -trimpath -ldflags "-s -w -X main.version=$Version" -o $out ./cmd/lx-music-mcp
    if ($LASTEXITCODE -ne 0) { throw "build failed: $goos/$goarch" }

    $size = [math]::Round((Get-Item $out).Length / 1MB, 2)
    Write-Host "  done, $size MB"
}

if ($All) {
    Build-One "windows" "amd64" "lx-music-mcp.exe"
    Build-One "windows" "arm64" "lx-music-mcp-arm64.exe"
    Build-One "linux"   "amd64" "lx-music-mcp-linux-amd64"
    Build-One "linux"   "arm64" "lx-music-mcp-linux-arm64"
    Build-One "darwin"  "arm64" "lx-music-mcp-darwin-arm64"
    Build-One "darwin"  "amd64" "lx-music-mcp-darwin-amd64"
} else {
    Build-One "windows" "amd64" "lx-music-mcp.exe"
}

Write-Host ""
Write-Host "output dir:  $outDir"
Write-Host "setup guide: $outDir\README.md"
