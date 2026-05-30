param(
    [string]$Server = "sub2api",
    [string]$RealchatRoot = (Resolve-Path "$PSScriptRoot\..\..\..").Path,
    [string]$Version = ("local-build-" + (Get-Date).ToUniversalTime().ToString("yyyyMMdd-HHmmss")),
    [int]$DrainSeconds = 120,
    [switch]$SkipTests
)

$ErrorActionPreference = "Stop"

$ProjectRoot = (Resolve-Path "$PSScriptRoot\..\..").Path
$TempRoot = Join-Path $env:TEMP "sub2api-bluegreen-deploy"
$BinaryPath = Join-Path $TempRoot "sub2api-linux-amd64"
$ArtifactPath = Join-Path $TempRoot "sub2api-local-build.tar.gz"
$RemoteArtifact = "/tmp/sub2api-local-build.tar.gz"
$RemoteScript = Join-Path $PSScriptRoot "remote-docker-bluegreen.sh"
$RemoteEnv = "/tmp/sub2api-bluegreen-deploy.env"
$LocalEnv = Join-Path $TempRoot "sub2api-bluegreen-deploy.env"

New-Item -ItemType Directory -Force -Path $TempRoot | Out-Null

if (-not $SkipTests) {
    Push-Location (Join-Path $ProjectRoot "backend")
    try {
        go test ./...
    } finally {
        Pop-Location
    }
}

pnpm --dir (Join-Path $ProjectRoot "frontend") run build

Push-Location (Join-Path $ProjectRoot "backend")
try {
    $env:CGO_ENABLED = "0"
    $env:GOOS = "linux"
    $env:GOARCH = "amd64"
    go build -tags embed -ldflags "-s -w -X main.BuildType=release" -trimpath -o $BinaryPath ./cmd/server
} finally {
    Remove-Item Env:\CGO_ENABLED -ErrorAction SilentlyContinue
    Remove-Item Env:\GOOS -ErrorAction SilentlyContinue
    Remove-Item Env:\GOARCH -ErrorAction SilentlyContinue
    Pop-Location
}

if (Test-Path $ArtifactPath) {
    Remove-Item -LiteralPath $ArtifactPath -Force
}
tar -czf $ArtifactPath -C $TempRoot sub2api-linux-amd64 -C (Join-Path $ProjectRoot "backend") resources

Push-Location $RealchatRoot
try {
    $envContent = @(
        "VERSION=$Version"
        "DRAIN_SECONDS=$DrainSeconds"
        "ARTIFACT=$RemoteArtifact"
    ) -join "`n"
    [System.IO.File]::WriteAllText($LocalEnv, $envContent + "`n", [System.Text.Encoding]::ASCII)
    agent-cli ssh upload $Server --local $ArtifactPath --remote $RemoteArtifact
    agent-cli ssh upload $Server --local $LocalEnv --remote $RemoteEnv
    agent-cli ssh run-script $Server --local $RemoteScript --cwd /root --timeout 900000 --max-output-bytes 30000
} finally {
    Pop-Location
}
