param(
    [Parameter(Mandatory)][string]$Version,
    [Parameter(Mandatory)][string]$PackageDirectory
)
$ErrorActionPreference = 'Stop'
if ($env:GITHUB_ACTIONS -ne 'true') { throw 'System installation smoke test runs only on a disposable GitHub runner' }
if ($Version -notmatch '^\d+\.\d+\.\d+(?:-[a-zA-Z0-9.]+)?$') { throw 'Invalid version' }
$projectRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$packages = (Resolve-Path -LiteralPath $PackageDirectory).Path
$work = Join-Path $projectRoot '.tools/installer-smoke'
New-Item -ItemType Directory -Force -Path $work | Out-Null
$installerOnWindows = [Environment]::OSVersion.Platform -eq [PlatformID]::Win32NT
$installerOnMac = (-not $installerOnWindows) -and ((uname -s) -eq 'Darwin')
$originalUserPath = $null
$installed = $false
function Invoke-Checked([string]$Program, [string[]]$Arguments) {
    & $Program @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Program failed: $LASTEXITCODE" }
}
try {
    if ($installerOnWindows) {
        $originalUserPath = [Environment]::GetEnvironmentVariable('Path','User')
        $package = Join-Path $packages "git-task_${Version}_windows_amd64.msi"
        $process = Start-Process msiexec.exe -ArgumentList @('/i', ('"' + $package + '"'), '/qn', '/norestart', '/L*V', ('"' + (Join-Path $work 'install.log') + '"')) -WindowStyle Hidden -Wait -PassThru
        if ($process.ExitCode -notin @(0,3010)) { Get-Content (Join-Path $work 'install.log') -Tail 80; throw "MSI installation failed: $($process.ExitCode)" }
        $installed = $true
        $newUserPath = [Environment]::GetEnvironmentVariable('Path','User')
        if ($newUserPath -eq $originalUserPath) { throw 'MSI did not add the user PATH entry' }
        $env:PATH += [IO.Path]::PathSeparator + $newUserPath
    } elseif ($installerOnMac) {
        Invoke-Checked 'sudo' @('/usr/sbin/installer','-pkg',(Join-Path $packages "git-task_${Version}_darwin_arm64.pkg"),'-target','/')
        $installed = $true
        if ((Get-Content -LiteralPath '/etc/paths.d/git-task') -ne '/usr/local/bin') { throw 'macOS PATH registration missing' }
    } else {
        Invoke-Checked 'sudo' @('dpkg','--install',(Join-Path $packages "git-task_${Version}_linux_amd64.deb"))
        $installed = $true
        Invoke-Checked 'rpm' @('--query','--package','--info',(Join-Path $packages "git-task_${Version}_linux_amd64.rpm"))
    }
    $actualVersion = & git task version
    if ($LASTEXITCODE -ne 0 -or $actualVersion -ne "git-task $Version") { throw 'Installed git task version differs' }
    Get-ChildItem Env: | Where-Object { $_.Name -like 'GIT_*' } | ForEach-Object { Remove-Item -LiteralPath "Env:$($_.Name)" }
    $emptyConfig = Join-Path $work 'empty.gitconfig'
    [IO.File]::WriteAllText($emptyConfig,'')
    $env:GIT_CONFIG_NOSYSTEM = '1'
    $env:GIT_CONFIG_GLOBAL = $emptyConfig
    $env:GIT_CONFIG_SYSTEM = $emptyConfig
    $env:GIT_AUTHOR_NAME = 'Installer Check'
    $env:GIT_AUTHOR_EMAIL = 'installer@example.invalid'
    $env:GIT_COMMITTER_NAME = $env:GIT_AUTHOR_NAME
    $env:GIT_COMMITTER_EMAIL = $env:GIT_AUTHOR_EMAIL
    $repository = Join-Path $work 'project'
    New-Item -ItemType Directory -Path $repository | Out-Null
    Set-Location -LiteralPath $repository
    Invoke-Checked 'git' @('init','--initial-branch=main','--template=')
    Invoke-Checked 'git' @('commit','--allow-empty','-m','Initial')
    Invoke-Checked 'git' @('task','init')
    Invoke-Checked 'git' @('task','add','Installed task')
    Invoke-Checked 'git' @('task','status')
    if (-not (Test-Path -LiteralPath '.git-task/plan.json')) { throw 'Init did not create the project plan' }
    Write-Output 'PASS: native installation followed by git task init without a project alias'
} finally {
    Set-Location -LiteralPath $projectRoot
    if ($installed -and $installerOnWindows) {
        $process = Start-Process msiexec.exe -ArgumentList @('/x', ('"' + $package + '"'), '/qn', '/norestart') -WindowStyle Hidden -Wait -PassThru
        if ($process.ExitCode -notin @(0,3010)) { throw 'MSI uninstall failed' }
        if ([Environment]::GetEnvironmentVariable('Path','User') -cne $originalUserPath) { throw 'Uninstall changed another user PATH entry' }
    } elseif ($installed -and -not $installerOnMac) {
        Invoke-Checked 'sudo' @('dpkg','--remove','git-task')
    }
}
