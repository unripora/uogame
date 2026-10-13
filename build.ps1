# UOGame backend build script (x86 / linux-amd64)
# Purpose: cross-compile Go backend -> assemble fnOS fpk project directory
# Note: only does "compile + assemble"; final fpk packing needs fnOS fnpack (see end)
#
# Usage:
#   powershell -ExecutionPolicy Bypass -File .\build.ps1

$ErrorActionPreference = "Stop"

# ---------- paths & tools ----------
$Root      = $PSScriptRoot
$ServerDir = Join-Path $Root "server"
$BuildDir  = Join-Path $Root "build"
$FpkOut    = Join-Path $BuildDir "fpk"              # fpk project root (framework + app/)
$AppDir    = Join-Path $FpkOut "app"                # app/ (ui + server)
$GoExe     = "D:\tools\go\bin\go.exe"
$WebDir    = Join-Path $Root "web"                 # 前端工程（可维护源码）
$WebDist   = Join-Path $WebDir "dist"              # 前端构建产物
$UiStatic  = Join-Path $Root "tmp\_fpk_extract\ui" # 原产物静态资源（emulator/images 等）
$FpkSource = Join-Path $Root "tmp\_fpk_extract"
$FnpackExe = Join-Path $Root "tmp\_fnpack\fnpack.exe"

Write-Host "==> UOGame build start" -ForegroundColor Cyan

# ---------- 0. precheck ----------
if (-not (Test-Path $GoExe))     { throw "Go not found: $GoExe" }
if (-not (Test-Path $WebDir))    { throw "Web project not found: $WebDir" }
if (-not (Test-Path $UiStatic))  { throw "UI static artifact not found: $UiStatic" }
if (-not (Test-Path $FpkSource)) { throw "Framework source not found: $FpkSource" }

# CN module proxy + isolated cache (avoid Windows index crash)
$env:GOMODCACHE = "D:\gocache\mod"
$env:GOCACHE    = "D:\gocache\build"
$env:GOTMPDIR   = "D:\gocache\tmp"
$env:GOPROXY    = "https://goproxy.cn,direct"

# ---------- 0.5 build frontend (web/ -> web/dist) ----------
Write-Host "==> [0/4] building frontend (vite) ..." -ForegroundColor Cyan
Push-Location $WebDir
try {
    if (-not (Test-Path (Join-Path $WebDir "node_modules"))) {
        Write-Host "    node_modules missing, running npm install ..." -ForegroundColor Yellow
        & npm install --registry=https://registry.npmmirror.com
        if ($LASTEXITCODE -ne 0) { throw "npm install failed, exit code $LASTEXITCODE" }
    }
    & npm run build
    if ($LASTEXITCODE -ne 0) { throw "frontend build failed, exit code $LASTEXITCODE" }
} finally {
    Pop-Location
}
if (-not (Test-Path (Join-Path $WebDist "index.html"))) { throw "frontend dist missing: $WebDist" }

# ---------- 1. assemble fpk project dir ----------
Write-Host "==> [1/4] assembling fpk project dir ..." -ForegroundColor Cyan
$AppDir = Join-Path $FpkOut "app"
if (Test-Path $FpkOut) { Remove-Item -Recurse -Force $FpkOut }
New-Item -ItemType Directory -Force -Path $AppDir | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $AppDir "server") | Out-Null

# framework plaintext files at fpk ROOT (manifest / cmd / config / wizard / icons)
Copy-Item (Join-Path $FpkSource "manifest")     (Join-Path $FpkOut "manifest")
Copy-Item (Join-Path $FpkSource "ICON.PNG")     (Join-Path $FpkOut "ICON.PNG")
Copy-Item (Join-Path $FpkSource "ICON_256.PNG") (Join-Path $FpkOut "ICON_256.PNG")
Copy-Item -Recurse (Join-Path $FpkSource "cmd")    (Join-Path $FpkOut "cmd")
Copy-Item -Recurse (Join-Path $FpkSource "config") (Join-Path $FpkOut "config")
Copy-Item -Recurse (Join-Path $FpkSource "wizard") (Join-Path $FpkOut "wizard")

# only ui + server go into app/ (fnpack packs app/ into app.tgz)
$UiTarget = Join-Path $AppDir "ui"
Copy-Item -Recurse $WebDist $UiTarget
# 补齐原产物的静态资源（构建产物未包含的部分），先删后拷避免目录嵌套
foreach ($name in @("emulator", "favicon.ico", "icon_192.png", "icon_64.png",
                    "manifest.json", "site.webmanifest", "alipay.png", "wechat.png", "ICON.PNG", "config")) {
    $src = Join-Path $UiStatic $name
    if (Test-Path $src) {
        $dstItem = Join-Path $UiTarget $name
        if (Test-Path $dstItem) { Remove-Item -Recurse -Force $dstItem }
        Copy-Item -Recurse -Force $src $dstItem
    }
}

# images 目录需"合并"而非覆盖：构建产物含 default-covers（默认封面），原产物含 icon_* 等
$imgSrc = Join-Path $UiStatic "images"
$imgDst = Join-Path $UiTarget "images"
if (Test-Path $imgSrc) {
    New-Item -ItemType Directory -Force -Path $imgDst | Out-Null
    Get-ChildItem -Path $imgSrc | ForEach-Object {
        Copy-Item -Recurse -Force $_.FullName (Join-Path $imgDst $_.Name)
    }
}

# ---------- 2. cross-compile backend (linux/amd64, static) ----------
Write-Host "==> [2/4] compiling backend (GOOS=linux GOARCH=amd64 CGO_ENABLED=0) ..." -ForegroundColor Cyan
$env:GOOS        = "linux"
$env:GOARCH      = "amd64"
$env:CGO_ENABLED = "0"

$OutBin = Join-Path $AppDir "server\server"
Push-Location $ServerDir
try {
    & $GoExe build -trimpath -ldflags="-s -w" -o $OutBin ./cmd/server
    if ($LASTEXITCODE -ne 0) { throw "build failed, exit code $LASTEXITCODE" }
} finally {
    Pop-Location
    Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
}

# ---------- 3. verify artifact ----------
Write-Host "==> [3/4] verifying artifact ..." -ForegroundColor Cyan
$bin = Get-Item $OutBin
$head = [System.IO.File]::ReadAllBytes($OutBin)[0..3]
$isElf = ($head[0] -eq 0x7F -and $head[1] -eq 0x45 -and $head[2] -eq 0x4C -and $head[3] -eq 0x46)
if (-not $isElf) { throw "artifact is not an ELF file, cross-compile abnormal" }

Write-Host ("    binary: {0:N2} MB (ELF amd64)" -f ($bin.Length / 1MB)) -ForegroundColor Green
Write-Host "    project dir: $AppDir" -ForegroundColor Green

# directory snapshot
Write-Host "`n==> fpk project structure:" -ForegroundColor Cyan
Get-ChildItem $AppDir | ForEach-Object {
    if ($_.PSIsContainer) { Write-Host ("    [D] " + $_.Name) }
    else                  { Write-Host ("    [F] " + $_.Name) }
}

Write-Host "`n==> compile & assemble done!" -ForegroundColor Green

# ---------- 4. pack with fnpack ----------
if (Test-Path $FnpackExe) {
    Write-Host "==> [4/4] packing fpk with fnpack ..." -ForegroundColor Cyan
    Push-Location $FpkOut
    try {
        & $FnpackExe build -d .
        if ($LASTEXITCODE -ne 0) { throw "fnpack build failed, exit code $LASTEXITCODE" }
    } finally {
        Pop-Location
    }
    $FpkFile = Join-Path $FpkOut "lite.game.fpk"
    if (Test-Path $FpkFile) {
        Write-Host ("    fpk: {0:N2} MB -> {1}" -f ((Get-Item $FpkFile).Length / 1MB) $FpkFile) -ForegroundColor Green
    }
} else {
    Write-Host "==> [4/4] fnpack not found, skipped packing." -ForegroundColor Yellow
    Write-Host "    put fnpack.exe at: $FnpackExe then run: fnpack build -d `"$FpkOut`"" -ForegroundColor Yellow
}

Write-Host "`n==> all done!" -ForegroundColor Green
