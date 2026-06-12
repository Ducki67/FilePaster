$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $root

$iconPath = Join-Path $root "icon.ico"
$sysoPath = Join-Path $root "rsrc_windows_amd64.syso"

if (-not (Test-Path $iconPath)) {
    throw "icon.ico not found in project root."
}

function Get-RsrcPath {
    $gobin = (go env GOBIN).Trim()
    if ($gobin) {
        $candidate = Join-Path $gobin "rsrc.exe"
        if (Test-Path $candidate) {
            return $candidate
        }
    }

    $gopath = (go env GOPATH).Trim()
    if ($gopath) {
        $candidate = Join-Path (Join-Path $gopath "bin") "rsrc.exe"
        if (Test-Path $candidate) {
            return $candidate
        }
    }

    return $null
}

$rsrc = Get-RsrcPath
if (-not $rsrc) {
    Write-Host "Installing rsrc tool..."
    go install github.com/akavel/rsrc@latest
    $rsrc = Get-RsrcPath
    if (-not $rsrc) {
        throw "rsrc install failed or rsrc.exe not found in Go bin path."
    }
}

Write-Host "Generating Windows resource file from icon.ico..."
& $rsrc -ico $iconPath -o $sysoPath

Write-Host "Building FilePaster.exe..."
go build -o FilePaster.exe .

Write-Host "Done. Built FilePaster.exe with embedded icon."
