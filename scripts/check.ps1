param(
    [ValidateSet('P15', 'P18')][string]$Stage = 'P15',
    [ValidateSet('Checks', 'Race', 'Fuzz', 'Security')][string]$Mode = 'Checks',
    [string]$Go = 'go',
    [string]$Govulncheck = 'govulncheck',
    [string]$VulnerabilityDatabase = 'https://vuln.go.dev',
    [string]$FuzzTime = '30s'
)

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $projectRoot
$goCommand = Get-Command -Name $Go -CommandType Application -ErrorAction Stop
$Go = $goCommand.Source
$goDirectory = Split-Path -Parent $Go
$env:PATH = $goDirectory + [IO.Path]::PathSeparator + $env:PATH
$work = Join-Path $projectRoot '.tools/checks'
foreach ($name in @('tmp', 'cache', 'modcache', 'gopath', 'home', 'empty-template')) {
    New-Item -ItemType Directory -Force -Path (Join-Path $work $name) | Out-Null
}
Get-ChildItem Env: | Where-Object { $_.Name -like 'GIT_*' } | ForEach-Object { Remove-Item -LiteralPath "Env:$($_.Name)" }
$env:GOCACHE = Join-Path $work 'cache'
$env:GOMODCACHE = Join-Path $work 'modcache'
$env:GOPATH = Join-Path $work 'gopath'
$env:GOTMPDIR = Join-Path $work 'tmp'
$env:TEMP = $env:GOTMPDIR
$env:TMP = $env:GOTMPDIR
$env:TMPDIR = $env:GOTMPDIR
$env:GOENV = 'off'
$env:GOTOOLCHAIN = 'local'
$env:GOTELEMETRY = 'off'
$env:GOPROXY = 'off'
$env:GOWORK = 'off'
$env:GOFLAGS = ''
$env:XDG_CONFIG_HOME = Join-Path $work 'home'
$env:GIT_CONFIG_NOSYSTEM = '1'
$env:GIT_CONFIG_GLOBAL = Join-Path $work 'empty.gitconfig'
$env:GIT_CONFIG_SYSTEM = $env:GIT_CONFIG_GLOBAL
[IO.File]::WriteAllText($env:GIT_CONFIG_GLOBAL, '')
$env:GIT_TEMPLATE_DIR = Join-Path $work 'empty-template'
$env:GIT_TERMINAL_PROMPT = '0'
$env:GIT_ASKPASS = 'git-task-no-askpass'
$env:GIT_ALLOW_PROTOCOL = 'file'
$env:GIT_AUTHOR_NAME = 'CI Test'
$env:GIT_AUTHOR_EMAIL = 'ci@example.invalid'
$env:GIT_COMMITTER_NAME = $env:GIT_AUTHOR_NAME
$env:GIT_COMMITTER_EMAIL = $env:GIT_AUTHOR_EMAIL

function Invoke-Checked([string]$Program, [string[]]$Arguments) {
    Write-Host "$Program $($Arguments -join ' ')"
    & $Program @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Program failed with exit code $LASTEXITCODE" }
}

Invoke-Checked $Go @('version')
Invoke-Checked 'git' @('--version')
$goVersion = & $Go env GOVERSION
if ($LASTEXITCODE -ne 0 -or $goVersion -notmatch '^go(\d+\.\d+\.\d+)') { throw 'Cannot determine Go version' }
if ([version]$Matches[1] -lt [version]'1.26.0') { throw 'Go 1.26.0 or newer is required' }
$gitVersion = & git --version
if ($LASTEXITCODE -ne 0 -or $gitVersion -notmatch '(\d+\.\d+\.\d+)') { throw 'Cannot determine Git version' }
if ([version]$Matches[1] -lt [version]'2.51.0') { throw 'Git 2.51.0 or newer is required' }
Write-Host "stage=$Stage mode=$Mode os=$([Environment]::OSVersion)"
Invoke-Checked $Go @('env', 'GOOS', 'GOARCH', 'CGO_ENABLED', 'CC')
Invoke-Checked 'git' @('rev-parse', 'HEAD')
Invoke-Checked 'git' @('status', '--short')
$groups = Get-Content -LiteralPath (Join-Path $PSScriptRoot 'p15-tests.json') -Raw | ConvertFrom-Json

switch ($Mode) {
    'Checks' {
        Invoke-Checked $Go @('vet', './...')
        if ($Stage -eq 'P18') {
            Invoke-Checked $Go @('test', '-count=1', '-timeout=20m', './...')
        } else {
            foreach ($group in $groups) {
                $pattern = '^(' + ($group.tests -join '|') + ')$'
                Invoke-Checked $Go @('test', '-count=1', '-timeout=15m', '-v', '-run', $pattern, $group.package)
            }
        }
        Invoke-Checked $Go @('build', './...')
    }
    'Race' {
        foreach ($group in $groups) {
            if ($group.race.Count -eq 0) { continue }
            $pattern = '^(' + ($group.race -join '|') + ')$'
            Invoke-Checked $Go @('test', '-race', '-count=1', '-timeout=15m', '-v', '-run', $pattern, $group.package)
        }
    }
    'Fuzz' {
        foreach ($target in @(
            @('./internal/task', 'FuzzPlanJSON'),
            @('./internal/task', 'FuzzPlanJSONAtomic'),
            @('./internal/task', 'FuzzQueuedAction'),
            @('./internal/hooks', 'FuzzHookInput')
        )) {
            Invoke-Checked $Go @('test', '-run=^$', "-fuzz=^$($target[1])$", "-fuzztime=$FuzzTime", '-parallel=2', $target[0])
        }
    }
    'Security' {
        Invoke-Checked $Govulncheck @('-version')
        Invoke-Checked $Govulncheck @('-db', $VulnerabilityDatabase, './...')
    }
}
