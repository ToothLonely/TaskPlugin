param(
    [ValidateSet('P15', 'P17', 'P18')][string]$Stage = 'P15',
    [ValidateSet('Checks', 'Race', 'Fuzz', 'Security')][string]$Mode = 'Checks',
    [string]$Go = 'go',
    [string]$Govulncheck = 'govulncheck',
    [string]$VulnerabilityDatabase = 'https://vuln.go.dev',
    [string]$FuzzTime = '30s',
    [string]$CandidateDirectory = '.tools/releases/0.1.0-rc.1-r02',
    [string]$P17Test = '',
    [string]$P17Subtest = ''
)

$ErrorActionPreference = 'Stop'
if (($P17Test -ne '' -or $P17Subtest -ne '') -and ($Stage -ne 'P17' -or $Mode -ne 'Checks')) {
    throw 'P17Test/P17Subtest require Stage P17 and Mode Checks'
}
if ($P17Subtest -ne '' -and $P17Test -eq '') { throw 'P17Subtest requires P17Test' }
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
$env:GOOS = ''
$env:GOARCH = ''
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
$manifestName = 'p15-tests.json'
if ($Stage -eq 'P17') {
    $manifestName = 'p17-tests.json'
    $release = (Resolve-Path -LiteralPath $CandidateDirectory).Path
    $manifestPath = Join-Path $release 'manifest.json'
    $manifestHash = (Get-FileHash -LiteralPath $manifestPath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($manifestHash -ne '1d2feeb2d0a45fcbdf0b9e2e214bf5ced4fb611bfc83c695e2b085680dbb0657') {
        throw 'P17 requires the accepted R02 manifest; update the candidate contract explicitly for another release'
    }
    $manifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
    $nativeOS = & $Go env GOOS
    if ($LASTEXITCODE -ne 0) { throw 'Cannot determine native OS' }
    $nativeArch = & $Go env GOARCH
    if ($LASTEXITCODE -ne 0) { throw 'Cannot determine native platform' }
    $platform = "$nativeOS/$nativeArch"
    $artifact = @($manifest.artifacts | Where-Object { $_.platform -eq $platform })
    if ($artifact.Count -ne 1) { throw "No candidate for $platform" }
    $archivePath = Join-Path $release $artifact[0].archive
    if ((Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant() -ne $artifact[0].sha256) {
        throw 'Candidate archive checksum differs from manifest'
    }
    $candidateRoot = Join-Path $work ('candidate-' + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $candidateRoot | Out-Null
    $binaryName = 'git-task'
    if ($nativeOS -eq 'windows') {
        Expand-Archive -LiteralPath $archivePath -DestinationPath $candidateRoot
        $binaryName += '.exe'
    } else {
        Invoke-Checked 'tar' @('-xzf', $archivePath, '-C', $candidateRoot)
    }
    $env:GIT_TASK_ACCEPTANCE_BINARY = Join-Path $candidateRoot $binaryName
    $env:GIT_TASK_ACCEPTANCE_SHA256 = $artifact[0].binary_sha256
    $env:GIT_TASK_ACCEPTANCE_VERSION = $manifest.version
    if ((Get-FileHash -LiteralPath $env:GIT_TASK_ACCEPTANCE_BINARY -Algorithm SHA256).Hash.ToLowerInvariant() -ne $env:GIT_TASK_ACCEPTANCE_SHA256) {
        throw 'Extracted candidate checksum differs from manifest'
    }
    Write-Host "candidate_manifest=$manifestHash platform=$platform binary_sha256=$env:GIT_TASK_ACCEPTANCE_SHA256"
} else {
    foreach ($name in @('GIT_TASK_ACCEPTANCE_BINARY', 'GIT_TASK_ACCEPTANCE_SHA256', 'GIT_TASK_ACCEPTANCE_VERSION')) {
        Remove-Item -LiteralPath "Env:$name" -ErrorAction SilentlyContinue
    }
}
$groups = Get-Content -LiteralPath (Join-Path $PSScriptRoot $manifestName) -Raw | ConvertFrom-Json
if ($P17Test -ne '') {
    $selected = @($groups | Where-Object { $_.tests -contains $P17Test })
    if ($selected.Count -ne 1) { throw "P17Test must name one test in $manifestName" }
    $groups = $selected
}

switch ($Mode) {
    'Checks' {
        Invoke-Checked $Go @('vet', './...')
        if ($Stage -eq 'P18') {
            Invoke-Checked $Go @('test', '-count=1', '-timeout=20m', './...')
        } else {
            foreach ($group in $groups) {
                if ($Stage -eq 'P17') {
                    $names = $group.tests
                    if ($P17Test -ne '') { $names = @($P17Test) }
                    foreach ($name in $names) {
                        $pattern = '^' + [regex]::Escape($name) + '$'
                        if ($P17Subtest -ne '') { $pattern += '/^' + [regex]::Escape($P17Subtest) + '$' }
                        Invoke-Checked $Go @('test', '-count=1', '-timeout=15m', '-parallel=1', '-v', '-run', $pattern, $group.package)
                    }
                } else {
                    $pattern = '^(' + ($group.tests -join '|') + ')$'
                    Invoke-Checked $Go @('test', '-count=1', '-timeout=15m', '-v', '-run', $pattern, $group.package)
                }
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
