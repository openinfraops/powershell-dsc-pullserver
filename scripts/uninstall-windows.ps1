param(
    [string]$InstallPath,
    [string]$RegistryKeyPath = "HKLM:\SOFTWARE\go-dsc-pull\DSC-Admin-Console",
    [string]$AppwizUninstallKeyPath = "HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\DSC-Admin-Console",
    [string]$ServiceName,
    [switch]$Force,
    [switch]$KeepConfig
)

$ErrorActionPreference = "Stop"

function Assert-Admin {
    $currentIdentity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($currentIdentity)
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        throw "Ce script doit etre execute en tant qu administrateur."
    }
}

function Get-RegistryValueOrNull {
    param(
        [Parameter(Mandatory = $true)][string]$KeyPath,
        [Parameter(Mandatory = $true)][string]$Name
    )

    if (-not (Test-Path -Path $KeyPath)) {
        return $null
    }

    try {
        $props = Get-ItemProperty -Path $KeyPath -ErrorAction Stop
        return $props.$Name
    }
    catch {
        return $null
    }
}

function Stop-AndDeleteServiceIfExists {
    param([Parameter(Mandatory = $true)][string]$Name)

    if ([string]::IsNullOrWhiteSpace($Name)) {
        return
    }

    $svc = Get-Service -Name $Name -ErrorAction SilentlyContinue
    if ($null -eq $svc) {
        Write-Host "Service non trouve: $Name"
        return
    }

    if ($svc.Status -ne 'Stopped') {
        Stop-Service -Name $Name -Force
        $svc.WaitForStatus('Stopped', [TimeSpan]::FromSeconds(60))
        Write-Host "Service arrete: $Name"
    }

    & sc.exe delete $Name | Out-Null
    Write-Host "Service supprime: $Name"
}

Assert-Admin

if ([string]::IsNullOrWhiteSpace($InstallPath)) {
    $InstallPath = Get-RegistryValueOrNull -KeyPath $RegistryKeyPath -Name "InstallPath"
}

if ([string]::IsNullOrWhiteSpace($ServiceName)) {
    $ServiceName = Get-RegistryValueOrNull -KeyPath $RegistryKeyPath -Name "ServiceName"
}

if ([string]::IsNullOrWhiteSpace($ServiceName)) {
    $ServiceName = "DSC-Console"
}

if (-not $Force) {
    $target = if ([string]::IsNullOrWhiteSpace($InstallPath)) { "l installation" } else { $InstallPath }
    $answer = Read-Host "Confirmer la desinstallation de $target ? (O/N)"
    if ($answer -notin @("O", "o", "Y", "y")) {
        Write-Host "Desinstallation annulee."
        exit 0
    }
}

Stop-AndDeleteServiceIfExists -Name $ServiceName

if (-not [string]::IsNullOrWhiteSpace($InstallPath) -and (Test-Path -Path $InstallPath -PathType Container)) {
    if ($KeepConfig -and (Test-Path -Path (Join-Path $InstallPath "config.json") -PathType Leaf)) {
        $backupDir = Join-Path $env:ProgramData "go-dsc-pull"
        if (-not (Test-Path -Path $backupDir)) {
            New-Item -Path $backupDir -ItemType Directory -Force | Out-Null
        }
        Copy-Item -Path (Join-Path $InstallPath "config.json") -Destination (Join-Path $backupDir "config.json") -Force
        Write-Host "config.json sauvegarde dans: $backupDir\\config.json"
    }

    Remove-Item -Path $InstallPath -Recurse -Force
    Write-Host "Dossier supprime: $InstallPath"
}

if (Test-Path -Path $RegistryKeyPath) {
    Remove-Item -Path $RegistryKeyPath -Recurse -Force
    Write-Host "Cle registre supprimee: $RegistryKeyPath"
}

if (Test-Path -Path $AppwizUninstallKeyPath) {
    Remove-Item -Path $AppwizUninstallKeyPath -Recurse -Force
    Write-Host "Entree Appwiz supprimee: $AppwizUninstallKeyPath"
}

Write-Host "Desinstallation terminee."
