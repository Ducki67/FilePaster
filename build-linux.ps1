$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $root

$targets = @(
    @{ GOOS = "linux"; GOARCH = "amd64"; OUT = "FilePaster-linux-amd64" },
    @{ GOOS = "linux"; GOARCH = "arm64"; OUT = "FilePaster-linux-arm64" }
)

foreach ($target in $targets) {
    Write-Host "Building $($target.OUT)..."
    $env:GOOS = $target.GOOS
    $env:GOARCH = $target.GOARCH
    $env:CGO_ENABLED = "0"
    go build -o $target.OUT .
}

Remove-Item Env:GOOS -ErrorAction SilentlyContinue
Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue

Write-Host "Done. Linux binaries: FilePaster-linux-amd64, FilePaster-linux-arm64"
