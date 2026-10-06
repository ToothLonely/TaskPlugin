param(
    [Parameter(Mandatory)][string]$Package,
    [Parameter(Mandatory)][string]$Destination
)
$ErrorActionPreference = 'Stop'
$installer = New-Object -ComObject WindowsInstaller.Installer
$database = $installer.OpenDatabase([IO.Path]::GetFullPath($Package), 0)
function Read-Scalar([string]$Sql) {
    $view = $database.OpenView($Sql)
    try { $view.Execute() | Out-Null; $record = $view.Fetch(); if ($null -eq $record) { throw 'Missing MSI value' }; return $record.StringData(1) } finally { $view.Close() | Out-Null }
}
try {
    if ((Read-Scalar 'SELECT `Value` FROM `Property` WHERE `Property`=''MSIINSTALLPERUSER''') -ne '1') { throw 'MSI must install per user' }
    if ((Read-Scalar 'SELECT `Name` FROM `Environment` WHERE `Environment`=''Path''') -ne '=-PATH') { throw 'MSI must own only its user PATH entry' }
    if ((Read-Scalar 'SELECT `Value` FROM `Environment` WHERE `Environment`=''Path''') -ne '[INSTALLDIR];[~]') { throw 'MSI must preserve existing PATH' }
    New-Item -ItemType Directory -Path $Destination | Out-Null
    $view = $database.OpenView('SELECT `Data` FROM `_Streams` WHERE `Name`=''payload.cab''')
    try {
        $view.Execute()
        $record = $view.Fetch()
        $stream = [IO.File]::Create((Join-Path $Destination 'payload.cab'))
        try {
            while ($true) {
                $chunk = $record.ReadStream(1, 65536, 1)
                if (-not $chunk.Length) { break }
                $bytes = New-Object byte[] $chunk.Length
                for ($i = 0; $i -lt $chunk.Length; $i++) { $bytes[$i] = [byte][char]$chunk[$i] }
                $stream.Write($bytes, 0, $bytes.Length)
            }
        } finally { $stream.Dispose() }
    } finally { $view.Close() }
    & "$env:SystemRoot/System32/expand.exe" '-F:*' (Join-Path $Destination 'payload.cab') $Destination | Out-Host
    if ($LASTEXITCODE -ne 0) { throw 'MSI payload extraction failed' }
    $view = $database.OpenView('SELECT `File`,`FileName` FROM `File`')
    try {
        $view.Execute() | Out-Null
        while ($record = $view.Fetch()) {
            $source = $record.StringData(1)
            $name = ($record.StringData(2) -split '\|')[-1]
            if ($source -notmatch '^Payload[0-3]$' -or $name -notin @('git-task.exe','README.md','GO-LICENSE','GO-PATENTS')) { throw 'Unexpected MSI payload name' }
            Move-Item -LiteralPath (Join-Path $Destination $source) -Destination (Join-Path $Destination $name)
        }
    } finally { $view.Close() | Out-Null }
} finally {
    [Runtime.InteropServices.Marshal]::FinalReleaseComObject($database) | Out-Null
    [Runtime.InteropServices.Marshal]::FinalReleaseComObject($installer) | Out-Null
}
