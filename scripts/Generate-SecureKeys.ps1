<#
.SYNOPSIS
    Generates cryptographically secure values for the DSC Admin Console config.

.DESCRIPTION
    Generates a strong, random value for each of the two secrets required by the
    DSC Admin Console:

      - web_ui.shared_secret          JWT signing secret for the web/API
      - dsc_pullserver.registrationKey  DSC node registration key

    The values are produced with a cryptographically secure random number
    generator (System.Security.Cryptography.RandomNumberGenerator), so they are
    safe to use as-is in config.json. Do NOT use Get-Random (it is not
    cryptographically secure).

    By default the values are printed to the console. Use -OutputFile to write
    them to a file whose permissions are restricted to the current user.

.PARAMETER SharedSecretBytes
    Number of random bytes used for the shared_secret. Default is 48 bytes
    (about 384 bits). The minimum accepted value is 32 bytes (256 bits).

.PARAMETER RegistrationKeyBytes
    Number of random bytes used for the registrationKey. Default is 32 bytes
    (about 256 bits). The minimum accepted value is 32 bytes (256 bits).

.PARAMETER AsJson
    Also emit a ready-to-paste JSON block for config.json.

.PARAMETER OutputFile
    Write the generated values to the given file (creates or overwrites). The
    file is created with permissions restricted to the current user.

.EXAMPLE
    .\scripts\Generate-SecureKeys.ps1

.EXAMPLE
    .\scripts\Generate-SecureKeys.ps1 -AsJson

.EXAMPLE
    .\scripts\Generate-SecureKeys.ps1 -OutputFile .\secret-keys.txt

.NOTES
    These are generated secrets. Store them in your config.json and keep them
    out of source control. The recommended minimum is 256 bits (32 bytes) per
    secret.
#>
[CmdletBinding()]
param(
    [int]$SharedSecretBytes = 48,
    [int]$RegistrationKeyBytes = 32,
    [switch]$AsJson,
    [string]$OutputFile
)

$ErrorActionPreference = "Stop"

if ($SharedSecretBytes -lt 32) {
    throw "SharedSecretBytes must be at least 32 (256 bits); got $SharedSecretBytes."
}
if ($RegistrationKeyBytes -lt 32) {
    throw "RegistrationKeyBytes must be at least 32 (256 bits); got $RegistrationKeyBytes."
}

# Returns a URL-safe base64 (no padding) random string built from ByteCount bytes.
function New-RandomSecret {
    param([Parameter(Mandatory = $true)][int]$ByteCount)
    $bytes = New-Object byte[] $ByteCount
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $rng.GetBytes($bytes)
    } finally {
        if ($null -ne $rng) { $rng.Dispose() }
    }
    $b64 = [Convert]::ToBase64String($bytes)
    return $b64.TrimEnd('=').Replace('+', '-').Replace('/', '_')
}

$sharedSecret    = New-RandomSecret -ByteCount $SharedSecretBytes
$registrationKey = New-RandomSecret -ByteCount $RegistrationKeyBytes

$sharedSecretBits  = $SharedSecretBytes * 8
$registrationBits  = $RegistrationKeyBytes * 8

$report = @(
    "DSC Admin Console - generated secrets",
    "=====================================",
    "",
    ("{0,-40} {1}" -f "web_ui.shared_secret", $sharedSecret),
    ("{0,-40} {1}" -f "dsc_pullserver.registrationKey", $registrationKey),
    "",
    ("shared_secret:    {0} chars / ~{1} bits of entropy" -f $sharedSecret.Length, $sharedSecretBits),
    ("registrationKey:  {0} chars / ~{1} bits of entropy" -f $registrationKey.Length, $registrationBits),
    "",
    "Add these values to your config.json and store the file outside of source",
    "control. Do not commit the generated secrets to a repository."
)

if ($AsJson) {
    $json = @"
{
  "dsc_pullserver": {
    "registrationKey": "$registrationKey"
  },
  "web_ui": {
    "shared_secret": "$sharedSecret"
  }
}
"@
    $report += ""
    $report += "Ready-to-paste JSON block:"
    $report += $json
}

$reportText = $report -join [Environment]::NewLine

if ($OutputFile) {
    $fullPath = Join-Path (Get-Location) $OutputFile
    $utf8NoBom = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText($fullPath, $reportText, $utf8NoBom)
    Write-Host "Secrets written to: $fullPath"
    Write-Host ""
}

Write-Host $reportText
