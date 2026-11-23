param(
    [Parameter(Mandatory = $true)]
    [string]$SourcePath,

    [string]$InstallPath,
    [string]$RegistryKeyPath = "HKLM:\SOFTWARE\go-dsc-pull\DSC-Admin-Console",
    [string]$ExeName = "DSC-Admin-Console.exe",
    [string]$ServiceName = "DSC-Console",
    [string]$MigrationScriptPath,
    [switch]$RunMigration,
    [switch]$OverwriteConfig,
    [switch]$NoServiceRestart
)

$ErrorActionPreference = "Stop"
$AppwizUninstallKeyPath = "HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\DSC-Admin-Console"

function Assert-Admin {
    $currentIdentity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($currentIdentity)
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw "Ce script doit etre execute en tant qu administrateur."
    }
}

function Resolve-AbsolutePath {
    param([Parameter(Mandatory = $true)][string]$Path)
    return (Resolve-Path -Path $Path).Path
}

function Get-InstallPathFromRegistry {
    param([Parameter(Mandatory = $true)][string]$KeyPath)

    if (-not (Test-Path -Path $KeyPath)) {
        return $null
    }

    $props = Get-ItemProperty -Path $KeyPath -ErrorAction Stop
    return $props.InstallPath
}

function Get-ServiceNameFromRegistry {
    param([Parameter(Mandatory = $true)][string]$KeyPath)

    if (-not (Test-Path -Path $KeyPath)) {
        return $null
    }

    $props = Get-ItemProperty -Path $KeyPath -ErrorAction Stop
    return $props.ServiceName
}

function Copy-DirectoryContent {
    param(
        [Parameter(Mandatory = $true)][string]$From,
        [Parameter(Mandatory = $true)][string]$To,
        [switch]$SkipConfig
    )

    if (-not (Test-Path -Path $To)) {
        throw "InstallPath introuvable: $To"
    }

    Get-ChildItem -Path $From -Force | ForEach-Object {
        if ($SkipConfig -and $_.PSIsContainer -eq $false -and $_.Name -ieq "config.json") {
            return
        }
        Copy-Item -Path $_.FullName -Destination $To -Recurse -Force
    }
}

function Stop-AppServiceIfExists {
    param([Parameter(Mandatory = $true)][string]$Name)

    $svc = Get-Service -Name $Name -ErrorAction SilentlyContinue
    if ($null -eq $svc) {
        Write-Host "Service non trouve, poursuite sans arret: $Name"
        return $false
    }

    if ($svc.Status -ne 'Stopped') {
        Stop-Service -Name $Name -Force
        $svc.WaitForStatus('Stopped', [TimeSpan]::FromSeconds(60))
    }

    Write-Host "Service arrete: $Name"
    return $true
}

function Start-AppServiceIfExists {
    param([Parameter(Mandatory = $true)][string]$Name)

    $svc = Get-Service -Name $Name -ErrorAction SilentlyContinue
    if ($null -eq $svc) {
        Write-Host "Service non trouve, aucun redemarrage: $Name"
        return
    }

    Start-Service -Name $Name
    $svc.Refresh()
    Write-Host "Service demarre: $Name"
}

function Run-MssqlMigrationFromConfig {
    param(
        [Parameter(Mandatory = $true)][string]$ConfigPath,
        [Parameter(Mandatory = $true)][string]$SqlPath
    )

    if (-not (Test-Path -Path $ConfigPath -PathType Leaf)) {
        throw "config.json introuvable: $ConfigPath"
    }

    if (-not (Test-Path -Path $SqlPath -PathType Leaf)) {
        throw "Script SQL introuvable: $SqlPath"
    }

    $sqlcmd = Get-Command sqlcmd -ErrorAction SilentlyContinue
    if ($null -eq $sqlcmd) {
        throw "sqlcmd n est pas disponible."
    }

    $cfg = Get-Content -Raw -Path $ConfigPath | ConvertFrom-Json
    if ($null -eq $cfg.database -or [string]::IsNullOrWhiteSpace($cfg.database.driver)) {
        throw "Configuration database invalide dans config.json"
    }

    if ($cfg.database.driver.ToLowerInvariant() -ne "mssql") {
        throw "Migration automatique supportee uniquement pour le driver mssql."
    }

    $server = "{0},{1}" -f $cfg.database.server, $cfg.database.port
    & $sqlcmd.Source -S $server -d $cfg.database.name -U $cfg.database.user -P $cfg.database.password -b -i $SqlPath
    if ($LASTEXITCODE -ne 0) {
        throw "Echec execution migration SQL."
    }

    Write-Host "Migration SQL executee: $SqlPath"
}

function Update-RegistryMetadata {
    param(
        [Parameter(Mandatory = $true)][string]$KeyPath,
        [Parameter(Mandatory = $true)][string]$PathValue,
        [string]$VersionValue,
        [string]$ServiceNameValue
    )

    if (-not (Test-Path -Path $KeyPath)) {
        New-Item -Path $KeyPath -Force | Out-Null
    }

    New-ItemProperty -Path $KeyPath -Name "InstallPath" -PropertyType String -Value $PathValue -Force | Out-Null
    New-ItemProperty -Path $KeyPath -Name "UpdatedAtUtc" -PropertyType String -Value ((Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")) -Force | Out-Null
    if ($ServiceNameValue) {
        New-ItemProperty -Path $KeyPath -Name "ServiceName" -PropertyType String -Value $ServiceNameValue -Force | Out-Null
    }
    if ($VersionValue) {
        New-ItemProperty -Path $KeyPath -Name "Version" -PropertyType String -Value $VersionValue -Force | Out-Null
    }
}

function Ensure-AppwizUninstallEntry {
    param(
        [Parameter(Mandatory = $true)][string]$KeyPath,
        [Parameter(Mandatory = $true)][string]$InstallPathValue,
        [Parameter(Mandatory = $true)][string]$ExePath,
        [Parameter(Mandatory = $true)][string]$ServiceNameValue,
        [string]$VersionValue
    )

    if (-not (Test-Path -Path $KeyPath)) {
        New-Item -Path $KeyPath -Force | Out-Null
    }

    $displayVersion = if ([string]::IsNullOrWhiteSpace($VersionValue)) { "Unknown" } else { $VersionValue }
    $uninstallScriptPath = Join-Path $InstallPathValue "scripts\uninstall-windows.ps1"
    $uninstallCmd = "powershell.exe -ExecutionPolicy Bypass -File `"$uninstallScriptPath`" -InstallPath `"$InstallPathValue`" -ServiceName `"$ServiceNameValue`""
    $quietUninstallCmd = $uninstallCmd + " -Force"

    $estimatedSizeKb = 0
    if (Test-Path -Path $InstallPathValue -PathType Container) {
        $estimatedSizeKb = [int]([Math]::Round(((Get-ChildItem -Path $InstallPathValue -Recurse -Force -File | Measure-Object -Property Length -Sum).Sum / 1KB), 0))
    }

    New-ItemProperty -Path $KeyPath -Name "DisplayName" -PropertyType String -Value "DSC Admin Console" -Force | Out-Null
    New-ItemProperty -Path $KeyPath -Name "DisplayVersion" -PropertyType String -Value $displayVersion -Force | Out-Null
    New-ItemProperty -Path $KeyPath -Name "Publisher" -PropertyType String -Value "go-dsc-pull" -Force | Out-Null
    New-ItemProperty -Path $KeyPath -Name "InstallLocation" -PropertyType String -Value $InstallPathValue -Force | Out-Null
    New-ItemProperty -Path $KeyPath -Name "DisplayIcon" -PropertyType String -Value $ExePath -Force | Out-Null
    New-ItemProperty -Path $KeyPath -Name "UninstallString" -PropertyType String -Value $uninstallCmd -Force | Out-Null
    New-ItemProperty -Path $KeyPath -Name "QuietUninstallString" -PropertyType String -Value $quietUninstallCmd -Force | Out-Null
    New-ItemProperty -Path $KeyPath -Name "NoModify" -PropertyType DWord -Value 1 -Force | Out-Null
    New-ItemProperty -Path $KeyPath -Name "NoRepair" -PropertyType DWord -Value 1 -Force | Out-Null
    New-ItemProperty -Path $KeyPath -Name "EstimatedSize" -PropertyType DWord -Value $estimatedSizeKb -Force | Out-Null
}

Assert-Admin

$source = Resolve-AbsolutePath -Path $SourcePath
if (-not (Test-Path -Path $source -PathType Container)) {
    throw "SourcePath invalide: $SourcePath"
}

if ([string]::IsNullOrWhiteSpace($InstallPath)) {
    $InstallPath = Get-InstallPathFromRegistry -KeyPath $RegistryKeyPath
}

if (-not $PSBoundParameters.ContainsKey('ServiceName')) {
    $serviceNameFromRegistry = Get-ServiceNameFromRegistry -KeyPath $RegistryKeyPath
    if (-not [string]::IsNullOrWhiteSpace($serviceNameFromRegistry)) {
        $ServiceName = $serviceNameFromRegistry
    }
}

if ([string]::IsNullOrWhiteSpace($InstallPath)) {
    throw "InstallPath manquant et aucune valeur trouvee dans le registre ($RegistryKeyPath)."
}

$resolvedInstallPath = [System.IO.Path]::GetFullPath($InstallPath)
$serviceWasHandled = $false

if (-not $NoServiceRestart) {
    $serviceWasHandled = Stop-AppServiceIfExists -Name $ServiceName
}

try {
    if ($RunMigration) {
        if ([string]::IsNullOrWhiteSpace($MigrationScriptPath)) {
            throw "-RunMigration requiert -MigrationScriptPath."
        }

        $migrationPath = if ([System.IO.Path]::IsPathRooted($MigrationScriptPath)) { $MigrationScriptPath } else { Join-Path $source $MigrationScriptPath }
        $migrationPath = Resolve-AbsolutePath -Path $migrationPath
        $configPath = Join-Path $resolvedInstallPath "config.json"
        Run-MssqlMigrationFromConfig -ConfigPath $configPath -SqlPath $migrationPath
    }

    $skipConfig = $false
    if ((Test-Path -Path (Join-Path $resolvedInstallPath "config.json")) -and (-not $OverwriteConfig)) {
        $skipConfig = $true
    }

    Copy-DirectoryContent -From $source -To $resolvedInstallPath -SkipConfig:$skipConfig

    $installedExePath = Join-Path $resolvedInstallPath $ExeName
    $version = $null
    if (Test-Path -Path $installedExePath -PathType Leaf) {
        $version = (Get-Item -Path $installedExePath).VersionInfo.ProductVersion
    }
    Update-RegistryMetadata -KeyPath $RegistryKeyPath -PathValue $resolvedInstallPath -VersionValue $version -ServiceNameValue $ServiceName
    Ensure-AppwizUninstallEntry -KeyPath $AppwizUninstallKeyPath -InstallPathValue $resolvedInstallPath -ExePath $installedExePath -ServiceNameValue $ServiceName -VersionValue $version

    Write-Host "Mise a jour terminee."
    Write-Host "InstallPath registre: $resolvedInstallPath"
    Write-Host "ServiceName registre: $ServiceName"
    Write-Host "Entree Appwiz mise a jour: $AppwizUninstallKeyPath"
    if ($skipConfig) {
        Write-Host "config.json existant conserve (utiliser -OverwriteConfig pour l ecraser)."
    }
}
finally {
    if (-not $NoServiceRestart -and $serviceWasHandled) {
        Start-AppServiceIfExists -Name $ServiceName
    }
}
