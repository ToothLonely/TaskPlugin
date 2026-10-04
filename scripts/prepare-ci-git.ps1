$ErrorActionPreference = 'Stop'
if ($env:GITHUB_ACTIONS -ne 'true') { throw 'This preparation script is for hosted CI only; use existing local tools' }
$projectRoot = Split-Path -Parent $PSScriptRoot
$destination = Join-Path $projectRoot '.tools/ci-git'
New-Item -ItemType Directory -Force -Path $destination | Out-Null
if ($IsWindows) {
    $archive = Join-Path $destination 'mingit.zip'
    Invoke-WebRequest 'https://github.com/git-for-windows/git/releases/download/v2.51.0.windows.1/MinGit-2.51.0-64-bit.zip' -OutFile $archive
    $hash = 'c2c955a21fa99889d83f485f24fa5d9a38fffc2d509d4022385510e11c26b250'
    if ((Get-FileHash $archive -Algorithm SHA256).Hash.ToLowerInvariant() -ne $hash) { throw 'MinGit checksum mismatch' }
    Expand-Archive -LiteralPath $archive -DestinationPath $destination -Force
    $gitBin = Join-Path $destination 'cmd'
} else {
    $archive = Join-Path $destination 'git.tar.gz'
    Invoke-WebRequest 'https://www.kernel.org/pub/software/scm/git/git-2.51.0.tar.gz' -OutFile $archive
    $hash = '3d531799d2cf2cac8e294ec6e3229e07bfca60dc6c783fe69e7712738bef7283'
    if ((Get-FileHash $archive -Algorithm SHA256).Hash.ToLowerInvariant() -ne $hash) { throw 'Git source checksum mismatch' }
    & tar -xzf $archive -C $destination
    if ($LASTEXITCODE -ne 0) { throw 'Git extraction failed' }
    $source = Join-Path $destination 'git-2.51.0'
    & make -C $source -j2 "prefix=$destination/install" NO_GETTEXT=YesPlease NO_TCLTK=YesPlease NO_CURL=YesPlease NO_OPENSSL=YesPlease install
    if ($LASTEXITCODE -ne 0) { throw 'Git build failed' }
    $gitBin = Join-Path $destination 'install/bin'
}
$gitBin | Out-File -FilePath $env:GITHUB_PATH -Encoding utf8 -Append
