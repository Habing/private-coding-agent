# Slice 2 端到端验证。前置:
#  - Docker Desktop 在跑
#  - pca/sandbox:base 镜像已 build (docker build -t pca/sandbox:base ../../sandbox/image)
#  - 当前目录 deploy/compose/, .env 已从 .env.example 复制
#
# 用法:
#   cd deploy\compose
#   pwsh ./test-e2e.ps1

$ErrorActionPreference = 'Stop'

# Docker 客户端把进度信息写到 stderr。Windows PowerShell 5.1 把 stderr 包装成
# NativeCommandError, 配合 ErrorActionPreference=Stop 会让脚本误以为命令失败。
# 用 helper 调用 docker, 显式按退出码判定成功/失败。
function Invoke-Docker {
    param([Parameter(ValueFromRemainingArguments=$true)] $DockerArgs)
    $prev = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    & docker @DockerArgs 2>&1 | Out-Null
    $code = $LASTEXITCODE
    $ErrorActionPreference = $prev
    if ($code -ne 0) {
        throw ("docker " + ($DockerArgs -join " ") + " failed exit=" + $code)
    }
}

if (-not (Test-Path .\.env)) {
    Copy-Item .env.example .env
    Write-Host "[setup] copied .env.example -> .env"
}

Write-Host "[1/8] starting compose ..."
Invoke-Docker compose up -d --build
Start-Sleep -Seconds 20

Write-Host "[2/8] inserting demo user via psql ..."
# demo123 的 bcrypt (Slice 1 验证过的真实 hash)
$hash = '$2a$10$WJBaC0mXl/yIgPXKW8WbPujOAidLdmaDPlduPdV8i11ZHaFvcgUrC'
docker compose exec -T postgres psql -U app -d app -c @"
INSERT INTO users (tenant_id, email, password_hash, name, role)
VALUES ((SELECT id FROM tenants WHERE slug='default'),
        'demo@example.com', '$hash', 'Demo', 'admin')
ON CONFLICT (tenant_id, email) DO NOTHING;
"@ | Out-Null

Write-Host "[3/8] login ..."
$body = '{"tenant":"default","email":"demo@example.com","password":"demo123"}'
$tok = (Invoke-RestMethod -Method POST -Uri http://localhost:8080/auth/login -ContentType application/json -Body $body).token
$H = @{ Authorization = "Bearer $tok" }

Write-Host "[4/8] create sandbox ..."
$sb = Invoke-RestMethod -Method POST -Uri http://localhost:8080/sandbox/sessions -Headers $H -ContentType application/json -Body '{}'
$id = $sb.id
Write-Host "  -> sandbox $id, status=$($sb.status)"
if ($sb.status -ne 'running') { throw "expected status=running, got $($sb.status)" }

Write-Host "[5/8] write file ..."
$content = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes("hello world from e2e"))
Invoke-RestMethod -Method PUT -Uri "http://localhost:8080/sandbox/sessions/$id/files?path=hello.txt" `
    -Headers $H -ContentType application/json `
    -Body (@{ content_base64 = $content } | ConvertTo-Json) | Out-Null

Write-Host "[6/8] exec cat ..."
$exec = Invoke-RestMethod -Method POST -Uri "http://localhost:8080/sandbox/sessions/$id/exec" `
    -Headers $H -ContentType application/json `
    -Body '{"cmd":["cat","/workspace/hello.txt"]}'
$out = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($exec.stdout_base64))
Write-Host "  -> stdout: $out (exit=$($exec.exit_code))"
if ($out -ne 'hello world from e2e') { throw "stdout mismatch" }

Write-Host "[7/8] destroy ..."
Invoke-RestMethod -Method DELETE -Uri "http://localhost:8080/sandbox/sessions/$id" -Headers $H | Out-Null

Write-Host "[8/8] verify 404 after destroy ..."
# Destroyed sandboxes are reported as not found at the API boundary
# (the runtime layer maps StatusDestroyed -> ErrSandboxNotFound).
try {
    Invoke-RestMethod -Method POST -Uri "http://localhost:8080/sandbox/sessions/$id/exec" `
        -Headers $H -ContentType application/json -Body '{"cmd":["true"]}'
    throw "expected 404 after destroy"
} catch {
    $code = $_.Exception.Response.StatusCode.value__
    if ($code -ne 404) {
        throw "expected 404 after destroy, got $code"
    }
}

Invoke-Docker compose down
Write-Host "`nE2E PASS"
