param(
    [Parameter(Mandatory)][string]$Payload,
    [Parameter(Mandatory)][string]$Output,
    [Parameter(Mandatory)][string]$Version,
    [Parameter(Mandatory)][string]$Assets
)

$ErrorActionPreference = 'Stop'
$work = Split-Path -Parent $Payload
$cab = Join-Path $work 'payload.cab'
$msiFileNames = @('git-task.exe', 'README.md', 'GOLICENS|GO-LICENSE', 'GOPATENT|GO-PATENTS')
$sources = @($Payload, (Join-Path $Assets 'README.md'), (Join-Path $Assets 'GO-LICENSE'), (Join-Path $Assets 'GO-PATENTS'))
$ddf = @('.OPTION EXPLICIT', '.Set Cabinet=on', '.Set Compress=on', '.Set CompressionType=MSZIP', '.Set MaxDiskSize=0', '.Set MaxCabinetSize=0', '.Set CabinetNameTemplate=payload.cab', ".Set DiskDirectoryTemplate=$work", '.Set InfFileName=NUL', '.Set RptFileName=NUL')
for ($i = 0; $i -lt $sources.Count; $i++) { $ddf += ('"' + $sources[$i] + '" "Payload' + $i + '"') }
$ddfPath = Join-Path $work 'payload.ddf'
[IO.File]::WriteAllLines($ddfPath, $ddf, [Text.Encoding]::Default)
& "$env:SystemRoot/System32/makecab.exe" /F $ddfPath | Out-Host
if ($LASTEXITCODE -ne 0) { throw 'Cabinet creation failed' }

$installer = New-Object -ComObject WindowsInstaller.Installer
$database = $installer.OpenDatabase([IO.Path]::GetFullPath($Output), 3)
function Execute-Sql([string]$Sql) {
    $view = $database.OpenView($Sql)
    try { $view.Execute() } finally { $view.Close(); [Runtime.InteropServices.Marshal]::FinalReleaseComObject($view) | Out-Null }
}
function Insert-Row([string]$Table, [string[]]$Columns, [object[]]$Values) {
    if ($Columns.Count -ne $Values.Count) { throw "MSI row column count differs: $Table" }
    $fields = ($Columns | ForEach-Object { '`' + $_ + '`' }) -join ','
    $data = ($Values | ForEach-Object { if ($null -eq $_) { 'NULL' } elseif ($_ -is [int]) { [string]$_ } else { "'" + ([string]$_).Replace("'", "''") + "'" } }) -join ','
    Execute-Sql ('INSERT INTO `' + $Table + '` (' + $fields + ') VALUES (' + $data + ')')
}
try {
    $schemas = @{
        Property = '`Property` CHAR(72) NOT NULL, `Value` CHAR(0) PRIMARY KEY `Property`'
        Directory = '`Directory` CHAR(72) NOT NULL, `Directory_Parent` CHAR(72), `DefaultDir` CHAR(255) NOT NULL PRIMARY KEY `Directory`'
        Component = '`Component` CHAR(72) NOT NULL, `ComponentId` CHAR(38), `Directory_` CHAR(72) NOT NULL, `Attributes` SHORT NOT NULL, `Condition` CHAR(255), `KeyPath` CHAR(72) PRIMARY KEY `Component`'
        Feature = '`Feature` CHAR(38) NOT NULL, `Feature_Parent` CHAR(38), `Title` CHAR(64), `Description` CHAR(255), `Display` SHORT, `Level` SHORT NOT NULL, `Directory_` CHAR(72), `Attributes` SHORT NOT NULL PRIMARY KEY `Feature`'
        FeatureComponents = '`Feature_` CHAR(38) NOT NULL, `Component_` CHAR(72) NOT NULL PRIMARY KEY `Feature_`, `Component_`'
        File = '`File` CHAR(72) NOT NULL, `Component_` CHAR(72) NOT NULL, `FileName` CHAR(255) NOT NULL, `FileSize` LONG NOT NULL, `Version` CHAR(72), `Language` CHAR(20), `Attributes` SHORT, `Sequence` SHORT NOT NULL PRIMARY KEY `File`'
        Media = '`DiskId` SHORT NOT NULL, `LastSequence` LONG NOT NULL, `DiskPrompt` CHAR(64), `Cabinet` CHAR(255), `VolumeLabel` CHAR(32), `Source` CHAR(72) PRIMARY KEY `DiskId`'
        Registry = '`Registry` CHAR(72) NOT NULL, `Root` SHORT NOT NULL, `Key` CHAR(255) NOT NULL, `Name` CHAR(255), `Value` CHAR(0), `Component_` CHAR(72) NOT NULL PRIMARY KEY `Registry`'
        Environment = '`Environment` CHAR(72) NOT NULL, `Name` CHAR(255) NOT NULL, `Value` CHAR(255), `Component_` CHAR(72) NOT NULL PRIMARY KEY `Environment`'
        Upgrade = '`UpgradeCode` CHAR(38) NOT NULL, `VersionMin` CHAR(20), `VersionMax` CHAR(20), `Language` CHAR(255), `Attributes` SHORT NOT NULL, `Remove` CHAR(255), `ActionProperty` CHAR(72) NOT NULL PRIMARY KEY `UpgradeCode`, `VersionMin`, `VersionMax`, `Language`, `Attributes`'
        LaunchCondition = '`Condition` CHAR(255) NOT NULL, `Description` CHAR(255) NOT NULL PRIMARY KEY `Condition`'
    }
    foreach ($sequenceTable in @('InstallExecuteSequence', 'InstallUISequence', 'AdminExecuteSequence', 'AdminUISequence')) { $schemas[$sequenceTable] = '`Action` CHAR(72) NOT NULL, `Condition` CHAR(255), `Sequence` SHORT PRIMARY KEY `Action`' }
    foreach ($table in $schemas.Keys) { Execute-Sql ('CREATE TABLE `' + $table + '` (' + $schemas[$table] + ')') }
    $numericVersion = ($Version -split '-')[0]
    $upgradeCode = '{C0C7D813-8DDD-4F2B-951A-E30DC27F643C}'
    $properties = @{
        ProductCode = '{' + [Guid]::NewGuid().ToString().ToUpperInvariant() + '}'
        UpgradeCode = $upgradeCode
        ProductName = 'Git Task'
        ProductVersion = $numericVersion
        ProductLanguage = '1033'
        Manufacturer = 'Git Task'
        ALLUSERS = '2'
        MSIINSTALLPERUSER = '1'
        ARPNOMODIFY = '1'
        ARPNOREPAIR = '1'
        ARPCOMMENTS = "Git Task $Version"
        SecureCustomProperties = 'OLDPRODUCTS;NEWERPRODUCTFOUND'
    }
    foreach ($name in $properties.Keys) { Insert-Row 'Property' @('Property','Value') @($name,$properties[$name]) }
    Insert-Row 'Directory' @('Directory','Directory_Parent','DefaultDir') @('TARGETDIR',$null,'SourceDir')
    Insert-Row 'Directory' @('Directory','Directory_Parent','DefaultDir') @('LocalAppDataFolder','TARGETDIR','.')
    Insert-Row 'Directory' @('Directory','Directory_Parent','DefaultDir') @('ProgramsDir','LocalAppDataFolder','Programs')
    Insert-Row 'Directory' @('Directory','Directory_Parent','DefaultDir') @('AppDir','ProgramsDir','GitTask')
    Insert-Row 'Directory' @('Directory','Directory_Parent','DefaultDir') @('INSTALLDIR','AppDir','bin')
    Insert-Row 'Component' @('Component','ComponentId','Directory_','Attributes','Condition','KeyPath') @('Binary','{14CA45AA-0877-4A14-A4B3-AD42A156A904}','INSTALLDIR',260,$null,'InstallPath')
    Insert-Row 'Feature' @('Feature','Feature_Parent','Title','Description','Display','Level','Directory_','Attributes') @('Main',$null,'Git Task',$null,0,1,'INSTALLDIR',0)
    Insert-Row 'FeatureComponents' @('Feature_','Component_') @('Main','Binary')
    for ($i = 0; $i -lt $sources.Count; $i++) {
        Insert-Row 'File' @('File','Component_','FileName','FileSize','Version','Language','Attributes','Sequence') @(('Payload' + $i),'Binary',$msiFileNames[$i],[int](Get-Item -LiteralPath $sources[$i]).Length,$null,$null,16384,($i + 1))
    }
    Insert-Row 'Media' @('DiskId','LastSequence','Cabinet') @(1,$sources.Count,'#payload.cab')
    Insert-Row 'Registry' @('Registry','Root','Key','Name','Value','Component_') @('InstallPath',1,'Software\GitTask','InstallPath','[INSTALLDIR]','Binary')
    Insert-Row 'Environment' @('Environment','Name','Value','Component_') @('Path','=-PATH','[INSTALLDIR];[~]','Binary')
    Insert-Row 'Upgrade' @('UpgradeCode','VersionMin','VersionMax','Language','Attributes','Remove','ActionProperty') @($upgradeCode,$null,$numericVersion,$null,513,$null,'OLDPRODUCTS')
    Insert-Row 'Upgrade' @('UpgradeCode','VersionMin','VersionMax','Language','Attributes','Remove','ActionProperty') @($upgradeCode,$numericVersion,$null,$null,2,$null,'NEWERPRODUCTFOUND')
    Insert-Row 'LaunchCondition' @('Condition','Description') @('Installed OR NOT NEWERPRODUCTFOUND','A newer Git Task version is already installed.')
    $execute = @{FindRelatedProducts=25;LaunchConditions=100;CostInitialize=800;FileCost=900;CostFinalize=1000;InstallValidate=1400;InstallInitialize=1500;ProcessComponents=1600;UnpublishFeatures=1800;RemoveRegistryValues=2600;RemoveEnvironmentStrings=3300;RemoveFiles=3500;InstallFiles=4000;WriteRegistryValues=5000;WriteEnvironmentStrings=5200;RegisterUser=6000;RegisterProduct=6100;PublishFeatures=6300;PublishProduct=6400;InstallExecute=6500;RemoveExistingProducts=6550;InstallFinalize=6600}
    foreach ($action in $execute.Keys) { Insert-Row 'InstallExecuteSequence' @('Action','Sequence') @($action,$execute[$action]) }
    $admin = @{CostInitialize=800;FileCost=900;CostFinalize=1000;InstallValidate=1400;InstallInitialize=1500;InstallAdminPackage=3900;InstallFiles=4000;InstallFinalize=6600}
    foreach ($action in $admin.Keys) { Insert-Row 'AdminExecuteSequence' @('Action','Sequence') @($action,$admin[$action]) }
    foreach ($table in @('InstallUISequence','AdminUISequence')) {
        foreach ($action in @('CostInitialize','FileCost','CostFinalize','ExecuteAction')) {
            $sequence = @{CostInitialize=800;FileCost=900;CostFinalize=1000;ExecuteAction=1300}[$action]
            Insert-Row $table @('Action','Sequence') @($action,$sequence)
        }
    }
    $view = $database.OpenView('INSERT INTO `_Streams` (`Name`,`Data`) VALUES (''payload.cab'',?)')
    $record = $installer.CreateRecord(1)
    $record.SetStream(1,$cab)
    try { $view.Execute($record) } finally { $view.Close(); [Runtime.InteropServices.Marshal]::FinalReleaseComObject($view) | Out-Null; [Runtime.InteropServices.Marshal]::FinalReleaseComObject($record) | Out-Null }
    $summary = $database.SummaryInformation(20)
    $summary.Property(2) = 'Git Task Installer'
    $summary.Property(3) = "Git Task $Version"
    $summary.Property(7) = 'x64;1033'
    $summary.Property(9) = '{' + [Guid]::NewGuid().ToString().ToUpperInvariant() + '}'
    $summary.Property(14) = 500
    $summary.Property(15) = 10
    $summary.Persist()
    [Runtime.InteropServices.Marshal]::FinalReleaseComObject($summary) | Out-Null
    $database.Commit()
} finally {
    [Runtime.InteropServices.Marshal]::FinalReleaseComObject($database) | Out-Null
    [Runtime.InteropServices.Marshal]::FinalReleaseComObject($installer) | Out-Null
}
