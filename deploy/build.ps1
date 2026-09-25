# 在 Windows 上打包发布包：编译 Linux 版后端、构建两个前端，输出 deploy\release\suno-release.tar.gz
# 用法（在项目根目录或 deploy 目录均可）：
#   powershell -ExecutionPolicy Bypass -File deploy\build.ps1
#   powershell -ExecutionPolicy Bypass -File deploy\build.ps1 -SkipOpen   # 不构建公开站

param([switch]$SkipOpen)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$out = Join-Path $PSScriptRoot 'release'
$pkg = Join-Path $out 'suno'

if (Test-Path $out) { Remove-Item -Recurse -Force $out }
New-Item -ItemType Directory -Force "$pkg\backend\internal\storage", "$pkg\admin", "$pkg\deploy" | Out-Null

Write-Host '==> 编译后端 (linux/amd64)'
Push-Location "$root\backend"
$env:GOOS = 'linux'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
try {
    go build -trimpath -ldflags '-s -w' -o "$pkg\backend\suno-server" ./cmd/server
    if ($LASTEXITCODE -ne 0) { throw 'go build 失败' }
} finally {
    Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED
    Pop-Location
}
Copy-Item "$root\backend\internal\storage\schema.sql" "$pkg\backend\internal\storage\"
Copy-Item "$root\backend\.env.example" "$pkg\backend\.env.example"

Write-Host '==> 构建运营后台'
Push-Location "$root\admin"
try {
    npm run build
    if ($LASTEXITCODE -ne 0) { throw 'admin 构建失败' }
} finally { Pop-Location }
Copy-Item -Recurse "$root\admin\dist\*" "$pkg\admin\"

if (-not $SkipOpen) {
    Write-Host '==> 构建公开站'
    Push-Location $root
    try {
        npm run build
        if ($LASTEXITCODE -ne 0) { throw '公开站构建失败' }
    } finally { Pop-Location }
    New-Item -ItemType Directory -Force "$pkg\open" | Out-Null
    Copy-Item -Recurse "$root\.output" "$pkg\open\.output"
}

Write-Host '==> 复制部署脚本'
Get-ChildItem $PSScriptRoot -File | Where-Object { $_.Name -ne 'build.ps1' } |
    ForEach-Object { Copy-Item $_.FullName "$pkg\deploy\" }
Copy-Item -Recurse "$PSScriptRoot\templates" "$pkg\deploy\templates"

# shell 脚本与模板统一转成 LF，避免在 Linux 上报 $'\r': command not found
Get-ChildItem "$pkg\deploy" -Recurse -File | Where-Object { $_.Extension -in '.sh', '.conf', '.service', '' } |
    ForEach-Object {
        $text = [IO.File]::ReadAllText($_.FullName) -replace "`r`n", "`n"
        [IO.File]::WriteAllText($_.FullName, $text, (New-Object Text.UTF8Encoding $false))
    }

Write-Host '==> 打包'
tar -czf "$out\suno-release.tar.gz" -C $out suno
if ($LASTEXITCODE -ne 0) { throw 'tar 打包失败' }

Write-Host ''
Write-Host "完成：$out\suno-release.tar.gz"
Write-Host '上传：scp deploy\release\suno-release.tar.gz root@服务器IP:/root/'
