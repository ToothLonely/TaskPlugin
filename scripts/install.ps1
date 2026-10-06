param(
    [string]$Version = '__GIT_TASK_VERSION__',
    [string]$FromDirectory = ''
)

$ErrorActionPreference = 'Stop'
if ([Environment]::OSVersion.Platform -ne [PlatformID]::Win32NT) { throw 'Use install.sh on Linux/macOS' }
$Version = $Version -replace '^v', ''
if ($Version -notmatch '^\d+\.\d+\.\d+(?:-[a-zA-Z0-9.]+)?$') { throw 'Specify -Version, for example 0.1.0' }
$architecture = $env:PROCESSOR_ARCHITEW6432
if (-not $architecture) { $architecture = $env:PROCESSOR_ARCHITECTURE }
if ($architecture -ne 'AMD64') { throw 'This installer supports Windows x64' }
$git = (Get-Command git -CommandType Application -ErrorAction Stop | Select-Object -First 1).Source
function Invoke-Git([string[]]$Arguments) {
    $result = & $git @Arguments
    if ($LASTEXITCODE -ne 0) { throw "Git failed: $($Arguments -join ' ')" }
    return $result
}
$gitVersion = Invoke-Git @('--version')
if ($gitVersion -notmatch '(\d+\.\d+\.\d+)' -or [version]$Matches[1] -lt [version]'2.51.0') { throw 'Git 2.51.0 or newer is required' }
$root = Invoke-Git @('rev-parse', '--show-toplevel')
$root = [IO.Path]::GetFullPath($root)
$managedRoot = Join-Path $root '.tools/git-task'
$destination = Join-Path $managedRoot ($Version + '/windows-amd64')
$binary = Join-Path $destination 'git-task.exe'
$alias = "!'" + $binary.Replace('\', '/').Replace("'", "'\''") + "'"
$oldAlias = & $git config --local --get alias.task
if ($LASTEXITCODE -notin @(0, 1)) { throw 'Cannot inspect existing task alias' }
if ($oldAlias) {
    $oldBinary = & $git config --local --get git-task.install-path
    if ($LASTEXITCODE -ne 0 -or -not $oldBinary) { throw 'Existing task alias belongs to another installation' }
    $oldAliasExpected = "!'" + $oldBinary.Replace('\', '/').Replace("'", "'\''") + "'"
    if ($oldAlias -ne $oldAliasExpected -or -not $oldBinary.StartsWith($managedRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
        throw 'Existing task alias is not managed by this installer'
    }
}
if (Test-Path -LiteralPath $destination) { throw 'This version already exists; choose a new version or inspect the installed directory' }
for ($path = $destination; $path -ne $root; $path = Split-Path -Parent $path) {
    if (Test-Path -LiteralPath $path) {
        $item = Get-Item -LiteralPath $path -Force
        if (-not $item.PSIsContainer -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw "Unsafe installation path: $path" }
    }
}
$tracked = Invoke-Git @('ls-files', '--', '.tools/git-task')
if ($tracked) { throw 'Installation directory contains tracked files' }
$exclude = Invoke-Git @('rev-parse', '--path-format=absolute', '--git-path', 'info/exclude')
$excludeParent = Split-Path -Parent $exclude
if (Test-Path -LiteralPath $excludeParent) {
    $excludeParentItem = Get-Item -LiteralPath $excludeParent -Force
    if (-not $excludeParentItem.PSIsContainer -or ($excludeParentItem.Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw 'Unsafe info directory' }
}
if (Test-Path -LiteralPath $exclude) {
    $excludeItem = Get-Item -LiteralPath $exclude -Force
    if ($excludeItem.PSIsContainer -or ($excludeItem.Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw 'Unsafe info/exclude path' }
}
$archiveName = "git-task_${Version}_windows_amd64.zip"
$temporary = Join-Path $root ('.tools/git-task-download-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $temporary | Out-Null
try {
    foreach ($name in @('SHA256SUMS', $archiveName)) {
        $target = Join-Path $temporary $name
        if ($FromDirectory) {
            Copy-Item -LiteralPath (Join-Path $FromDirectory $name) -Destination $target
        } else {
            $url = "https://github.com/ToothLonely/TaskPlugin/releases/download/v$Version/$name"
            Invoke-WebRequest -Uri $url -OutFile $target -UseBasicParsing
        }
    }
    $pattern = '^([a-fA-F0-9]{64})  ' + [regex]::Escape($archiveName) + '$'
    $matching = @(Get-Content -LiteralPath (Join-Path $temporary 'SHA256SUMS') | Where-Object { $_ -match $pattern })
    if ($matching.Count -ne 1) { throw 'Missing or duplicate archive checksum' }
    $expected = [regex]::Match($matching[0], $pattern).Groups[1].Value
    $archive = Join-Path $temporary $archiveName
    if ((Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash -ne $expected) { throw 'Archive checksum mismatch' }
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $zip = [IO.Compression.ZipFile]::OpenRead($archive)
    try {
        $entries = @($zip.Entries | Where-Object { $_.FullName -eq 'git-task.exe' })
        if ($entries.Count -ne 1) { throw 'Archive must contain exactly one git-task.exe' }
        [IO.Compression.ZipFileExtensions]::ExtractToFile($entries[0], (Join-Path $temporary 'git-task.exe'))
    } finally { $zip.Dispose() }
    $downloaded = Join-Path $temporary 'git-task.exe'
    $reportedVersion = & $downloaded version
    if ($LASTEXITCODE -ne 0 -or $reportedVersion -ne "git-task $Version") { throw 'Downloaded binary version differs from requested version' }
    if ($oldAlias -and $oldBinary -ne $binary) {
        throw 'Uninstall hooks with the old binary and remove its local task alias/install-path before switching versions; see INSTALL.md'
    }
    New-Item -ItemType Directory -Path $destination | Out-Null
    Copy-Item -LiteralPath $downloaded -Destination $binary
    New-Item -ItemType Directory -Path $excludeParent -Force | Out-Null
    $lines = @()
    if (Test-Path -LiteralPath $exclude) { $lines = @(Get-Content -LiteralPath $exclude) }
    if ($lines -notcontains '/.tools/git-task/') { [IO.File]::AppendAllText($exclude, "`n/.tools/git-task/`n", (New-Object Text.UTF8Encoding($false))) }
    Invoke-Git @('config', '--local', 'git-task.install-path', $binary) | Out-Null
    Invoke-Git @('config', '--local', 'alias.task', $alias) | Out-Null
    Write-Host "Installed git-task $Version for $root"
    Write-Host 'Next: git task init --target main; git task hooks install'
} finally {
    $resolvedTemporary = [IO.Path]::GetFullPath($temporary)
    if ($resolvedTemporary.StartsWith($root + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
        Remove-Item -LiteralPath $resolvedTemporary -Recurse -Force
    }
}
