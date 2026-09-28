param([string]$Docker = 'docker')

$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $PSScriptRoot
$checkImage = 'git-task-checks:go1.26.8-git2.51.0'

# Image preparation downloads tools. The actual checks run without networking.
$ErrorActionPreference = 'Continue' # Windows PowerShell treats native stderr as errors.
& $Docker build --progress plain -t $checkImage -f (Join-Path $PSScriptRoot 'linux-checks.Dockerfile') $PSScriptRoot
$ErrorActionPreference = 'Stop'
if ($LASTEXITCODE -ne 0) { throw "Linux check image build failed: $LASTEXITCODE" }

$dockerArgs = @(
    'run', '--rm', '--network', 'none', '--read-only', '--cap-drop', 'ALL',
    '--security-opt', 'no-new-privileges', '--user', '1000:1000',
    '--tmpfs', '/work:rw,exec,nosuid,size=4g,mode=1777',
    '--tmpfs', '/tmp:rw,exec,nosuid,size=1g,mode=1777',
    '--mount', "type=bind,source=$projectRoot/go.mod,target=/source/go.mod,readonly",
    '--mount', "type=bind,source=$projectRoot/cmd,target=/source/cmd,readonly",
    '--mount', "type=bind,source=$projectRoot/internal,target=/source/internal,readonly",
    $checkImage
)
$ErrorActionPreference = 'Continue'
& $Docker @dockerArgs
$ErrorActionPreference = 'Stop'
if ($LASTEXITCODE -ne 0) { throw "Linux checks failed: $LASTEXITCODE" }
