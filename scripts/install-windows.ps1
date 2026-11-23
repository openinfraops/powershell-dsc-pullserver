param(
    [Parameter(Mandatory = $true)]
    [string]$SourcePath,

    [string]$InstallPath = "$env:ProgramFiles\DSC-Admin-Console",
    [string]$RegistryKeyPath = "HKLM:\SOFTWARE\go-dsc-pull\DSC-Admin-Console",
    [string]$ExeName = "DSC-Admin-Console.exe",
    [string]$ServiceName = "DSC-Console",
    [string]$DisplayName = "DSC-Admin-Console",
    [switch]$CreateService,
    [switch]$OverwriteConfig
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

function Copy-DirectoryContent {
    param(
        [Parameter(Mandatory = $true)][string]$From,
        [Parameter(Mandatory = $true)][string]$To,
        [switch]$SkipConfig
    )

    if (-not (Test-Path -Path $To)) {
        New-Item -ItemType Directory -Path $To -Force | Out-Null
    }

    Get-ChildItem -Path $From -Force | ForEach-Object {
        if ($SkipConfig -and $_.PSIsContainer -eq $false -and $_.Name -ieq "config.json") {
            return
        }
        Copy-Item -Path $_.FullName -Destination $To -Recurse -Force
    }
}

function Ensure-RegistryInstallPath {
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
    New-ItemProperty -Path $KeyPath -Name "InstalledAtUtc" -PropertyType String -Value ((Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")) -Force | Out-Null
    if ($ServiceNameValue) {
        New-ItemProperty -Path $KeyPath -Name "ServiceName" -PropertyType String -Value $ServiceNameValue -Force | Out-Null
    }
    if ($VersionValue) {
        New-ItemProperty -Path $KeyPath -Name "Version" -PropertyType String -Value $VersionValue -Force | Out-Null
    }
}

function Ensure-Service {
    param(
        [Parameter(Mandatory = $true)][string]$Name,
        [Parameter(Mandatory = $true)][string]$BinaryPath,
        [string]$DisplayName = $Name
    )

    $existing = Get-Service -Name $Name -ErrorAction SilentlyContinue
    $binPathWithQuotes = '"' + $BinaryPath + '"'

    if ($null -eq $existing) {
        New-Service -Name $Name -BinaryPathName $binPathWithQuotes -DisplayName $DisplayName -StartupType Automatic | Out-Null
        Write-Host "Service cree: $Name (affichage: $DisplayName)"
    }
    else {
        & sc.exe config $Name binPath= $binPathWithQuotes start= auto DisplayName= $DisplayName | Out-Null
        Write-Host "Service mis a jour: $Name (affichage: $DisplayName)"
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

$expectedExe = Join-Path $source $ExeName
if (-not (Test-Path -Path $expectedExe -PathType Leaf)) {
    throw "Executable introuvable dans la source: $expectedExe"
}

$resolvedInstallPath = [System.IO.Path]::GetFullPath($InstallPath)
if (-not (Test-Path -Path $resolvedInstallPath)) {
    New-Item -ItemType Directory -Path $resolvedInstallPath -Force | Out-Null
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

Ensure-RegistryInstallPath -KeyPath $RegistryKeyPath -PathValue $resolvedInstallPath -VersionValue $version -ServiceNameValue $ServiceName
Ensure-AppwizUninstallEntry -KeyPath $AppwizUninstallKeyPath -InstallPathValue $resolvedInstallPath -ExePath $installedExePath -ServiceNameValue $ServiceName -VersionValue $version

if ($CreateService) {
    Ensure-Service -Name $ServiceName -BinaryPath $installedExePath -DisplayName $DisplayName
}

Write-Host "Installation terminee."
Write-Host "InstallPath enregistre dans le registre: $resolvedInstallPath"
Write-Host "ServiceName enregistre dans le registre: $ServiceName"
Write-Host "Entree Appwiz enregistree: $AppwizUninstallKeyPath"
if ($skipConfig) {
    Write-Host "config.json existant conserve (utiliser -OverwriteConfig pour l ecraser)."
}
