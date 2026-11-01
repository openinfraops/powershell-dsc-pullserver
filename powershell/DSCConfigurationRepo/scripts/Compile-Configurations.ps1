[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$SourcePath,
    [Parameter(Mandatory = $true)][string]$OutputPath,
    [string]$NodeContextPath = '',
    [string]$ConfigurationNamesJson = ''
)

$ErrorActionPreference = 'Stop'
Import-Module PSDesiredStateConfiguration -ErrorAction Stop

function ConvertTo-Hashtable {
    param([Parameter(ValueFromPipeline = $true)]$InputObject)

    process {
        if ($null -eq $InputObject) {
            return $null
        }
        if ($InputObject -is [System.Collections.IDictionary]) {
            $result = @{}
            foreach ($key in $InputObject.Keys) {
                $result[$key] = ConvertTo-Hashtable $InputObject[$key]
            }
            return $result
        }
        if ($InputObject -is [pscustomobject]) {
            $result = @{}
            foreach ($property in $InputObject.PSObject.Properties) {
                $result[$property.Name] = ConvertTo-Hashtable $property.Value
            }
            return $result
        }
        if ($InputObject -is [array]) {
            return ,@($InputObject | ForEach-Object { ConvertTo-Hashtable $_ })
        }
        return $InputObject
    }
}

$sourceRoot = (Resolve-Path -LiteralPath $SourcePath).Path
$outputRoot = [System.IO.Path]::GetFullPath($OutputPath)
New-Item -ItemType Directory -Path $outputRoot -Force | Out-Null
$nodeContext = [pscustomobject]@{ node_name = ''; configuration_names = @(); tags = @{} }
$hasNodeContext = -not [string]::IsNullOrWhiteSpace($NodeContextPath)
if ($hasNodeContext) {
    $nodeContext = ConvertTo-Hashtable (Get-Content -LiteralPath $NodeContextPath -Raw | ConvertFrom-Json)
}
$nodeName = [string]$nodeContext.node_name
$requestedNames = @($nodeContext.configuration_names)
if (-not [string]::IsNullOrWhiteSpace($ConfigurationNamesJson)) {
    $parsedNames = ConvertFrom-Json -InputObject $ConfigurationNamesJson
    if ($parsedNames -isnot [array]) {
        throw 'ConfigurationNamesJson must be a JSON array of strings.'
    }
    $requestedNames = @()
    foreach ($requestedName in $parsedNames) {
        if ($requestedName -isnot [string]) {
            throw 'ConfigurationNamesJson must be a JSON array of strings.'
        }
        $requestedNames += $requestedName
    }
}
$nodeTags = $nodeContext.tags

function Test-ManifestMatch {
    param([System.Collections.IDictionary]$Match, [System.Collections.IDictionary]$Tags)

    foreach ($key in $Match.Keys) {
        if (-not $Tags.ContainsKey([string]$key)) {
            return $false
        }
        $expectedValues = @($Match[$key] | ForEach-Object { [string]$_ })
        $actualValues = @($Tags[$key] | ForEach-Object { [string]$_ })
        if (-not (@($expectedValues | Where-Object { $actualValues -contains $_ }).Count)) {
            return $false
        }
    }
    return $true
}

function Merge-ConfigurationData {
    param([System.Collections.IDictionary]$Target, [System.Collections.IDictionary]$Overlay)

    foreach ($key in $Overlay.Keys) {
        $incoming = $Overlay[$key]
        if ([string]$key -match '^[^:]+::[^:]+$') {
            $currentResources = [ordered]@{}
            if ($Target[$key] -is [System.Collections.IDictionary]) {
                foreach ($resourceKey in $Target[$key].Keys) {
                    $currentResources[$resourceKey] = $Target[$key][$resourceKey]
                }
            } elseif ($Target[$key] -is [array]) {
                foreach ($resource in $Target[$key]) {
                    if (-not $resource.Name) { throw "Resource entry under '$key' must define Name." }
                    $currentResources[[string]$resource.Name] = $resource
                }
            }

            $incomingResources = [ordered]@{}
            if ($incoming -is [System.Collections.IDictionary]) {
                foreach ($resourceKey in $incoming.Keys) {
                    $incomingResources[$resourceKey] = $incoming[$resourceKey]
                }
            } elseif ($incoming -is [array]) {
                foreach ($resource in $incoming) {
                    if (-not $resource.Name) { throw "Resource entry under '$key' must define Name." }
                    $incomingResources[[string]$resource.Name] = $resource
                }
            } else {
                throw "Resource group '$key' must be an array or a dictionary."
            }

            foreach ($resourceKey in $incomingResources.Keys) {
                $currentResources[$resourceKey] = $incomingResources[$resourceKey]
            }
            $Target[$key] = $currentResources
            continue
        }
        if ($incoming -is [System.Collections.IDictionary] -and $Target[$key] -is [System.Collections.IDictionary]) {
            Merge-ConfigurationData -Target $Target[$key] -Overlay $incoming
            continue
        }
        $Target[$key] = $incoming
    }
}

function Get-LegacyTagValue {
    param([System.Collections.IDictionary]$Tags, [string[]]$Names)
    foreach ($name in $Names) {
        foreach ($key in $Tags.Keys) {
            if ([string]::Equals([string]$key, $name, [System.StringComparison]::OrdinalIgnoreCase)) {
                $values = [System.Collections.Generic.List[string]]::new()
                foreach ($value in @($Tags[$key])) {
                    Add-LegacyTagScalarValues -InputObject $value -Values $values
                }
                return @($values | Select-Object -Unique)
            }
        }
    }
    return @()
}

function Add-LegacyTagScalarValues {
    param($InputObject, [System.Collections.Generic.List[string]]$Values)

    if ($null -eq $InputObject) {
        return
    }
    if ($InputObject -is [System.Collections.IDictionary]) {
        foreach ($value in $InputObject.Values) {
            Add-LegacyTagScalarValues -InputObject $value -Values $Values
        }
        return
    }
    if ($InputObject -is [array]) {
        foreach ($value in $InputObject) {
            Add-LegacyTagScalarValues -InputObject $value -Values $Values
        }
        return
    }
    $text = ([string]$InputObject).Trim()
    if ($text) {
        $Values.Add($text)
    }
}

function Merge-LegacyDefinitionFile {
    param([System.Collections.IDictionary]$Merged, [string]$Path)
    if (Test-Path -LiteralPath $Path -PathType Leaf) {
        Write-Host "Loading PSD1: $Path"
        $definition = Import-PowerShellDataFile -LiteralPath $Path
        Merge-ConfigurationData -Target $Merged -Overlay $definition
    } else {
        Write-Host "PSD1 not found (optional): $Path"
    }
}

function Invoke-LegacyDSCCompilation {
    param(
        [string]$DefinitionsRoot,
        [string]$ConfigurationsRoot,
        [string]$NodeName,
        [System.Collections.IDictionary]$Tags,
        [string]$OutputPath
    )

    if ([string]::IsNullOrWhiteSpace($NodeName)) {
        throw 'The legacy dsc-config layout requires node_name in the node context.'
    }
    $os = Get-LegacyTagValue -Tags $Tags -Names @('OS', 'OperatingSystem') | Select-Object -First 1
    $environment = Get-LegacyTagValue -Tags $Tags -Names @('Environnement', 'Environment') | Select-Object -First 1
    $application = Get-LegacyTagValue -Tags $Tags -Names @('Application') | Select-Object -First 1
    $component = Get-LegacyTagValue -Tags $Tags -Names @('Composant', 'Component') | Select-Object -First 1
    $hostgroups = @(Get-LegacyTagValue -Tags $Tags -Names @('Hostgroup', 'HostGroup', 'Hostgroups') | ForEach-Object { $_ -split '[,;]' } | ForEach-Object { $_.Trim() } | Where-Object { $_ } | Select-Object -Unique)
    Write-Host "Node '$NodeName' selectors: OS='$os'; Environment='$environment'; Application='$application'; Component='$component'; Hostgroups='$($hostgroups -join ',')'"

    $definitionPaths = [System.Collections.Generic.List[string]]::new()
    if ($os) { $definitionPaths.Add((Join-Path $DefinitionsRoot (Join-Path 'os' ($os + '.psd1')))) }
    if ($environment) { $definitionPaths.Add((Join-Path $DefinitionsRoot (Join-Path 'environnement' ($environment + '.psd1')))) }
    foreach ($hostgroup in $hostgroups) {
        $definitionPaths.Add((Join-Path $DefinitionsRoot (Join-Path 'hostgroup' ($hostgroup + '.psd1'))))
    }
    if ($application) {
        $definitionPaths.Add((Join-Path $DefinitionsRoot (Join-Path 'application' ($application + '.psd1'))))
        if ($component) {
            $definitionPaths.Add((Join-Path $DefinitionsRoot (Join-Path (Join-Path 'application' $application) ($component + '.psd1'))))
        }
    }
    $definitionPaths.Add((Join-Path $DefinitionsRoot (Join-Path 'node' ($NodeName + '.psd1'))))

    $merged = [ordered]@{}
    foreach ($definitionPath in $definitionPaths) {
        Merge-LegacyDefinitionFile -Merged $merged -Path $definitionPath
    }
    if ($merged.Count -eq 0) {
        Write-Warning "No legacy definitions found for '$NodeName'; skipping this node."
        return
    }

    foreach ($moduleFile in Get-ChildItem -LiteralPath $ConfigurationsRoot -Filter '*.psm1' -File) {
        Write-Host "Loading PSM1: $($moduleFile.FullName)"
        Import-Module -Name $moduleFile.FullName -Force -ErrorAction Stop
    }
    foreach ($configurationFunction in @($merged.Keys | ForEach-Object { ([string]$_).Split('::')[0] } | Select-Object -Unique)) {
        if (-not (Get-Command -Name $configurationFunction -CommandType Configuration -ErrorAction SilentlyContinue)) {
            throw "Legacy DSC Configuration '$configurationFunction' was not found in configurations/*.psm1."
        }
    }

    Configuration NodeDefinition {
        param([string]$NodeName, [System.Collections.IDictionary]$Configurations)
        Import-DscResource -ModuleName PSDesiredStateConfiguration
        Node $NodeName {
            foreach ($functionName in @($Configurations.Keys | ForEach-Object { ([string]$_).Split('::')[0] } | Select-Object -Unique)) {
                $configurationCommand = Get-Command -Name $functionName -CommandType Configuration -ErrorAction Stop
                & $configurationCommand -nodename $NodeName -configurations $Configurations
            }
        }
    }

    $nodeOutput = Join-Path $OutputPath $NodeName
    New-Item -ItemType Directory -Path $nodeOutput -Force | Out-Null
    NodeDefinition -NodeName $NodeName -Configurations $merged -OutputPath $nodeOutput
    $mofFiles = @(Get-ChildItem -LiteralPath $nodeOutput -Filter '*.mof' -File -Recurse)
    if ($mofFiles.Count -ne 1) {
        throw "Legacy compiler must generate exactly one MOF for '$NodeName'; found $($mofFiles.Count)."
    }
    Move-Item -LiteralPath $mofFiles[0].FullName -Destination (Join-Path $OutputPath ($NodeName + '.mof')) -Force
}

$legacyRoot = ''
if (Test-Path -LiteralPath (Join-Path $sourceRoot 'configs/definitions') -PathType Container) {
    $legacyRoot = Join-Path $sourceRoot 'configs'
} elseif (Test-Path -LiteralPath (Join-Path $sourceRoot 'definitions') -PathType Container) {
    $legacyRoot = $sourceRoot
}
if ($legacyRoot) {
    if ($nodeContext.nodes) {
        foreach ($node in $nodeContext.nodes) {
            Invoke-LegacyDSCCompilation -DefinitionsRoot (Join-Path $legacyRoot 'definitions') -ConfigurationsRoot (Join-Path $legacyRoot 'configurations') -NodeName ([string]$node.node_name) -Tags $node.tags -OutputPath $outputRoot
        }
    } else {
        Invoke-LegacyDSCCompilation -DefinitionsRoot (Join-Path $legacyRoot 'definitions') -ConfigurationsRoot (Join-Path $legacyRoot 'configurations') -NodeName $nodeName -Tags $nodeTags -OutputPath $outputRoot
    }
    return
}

$manifests = @(Get-ChildItem -LiteralPath (Join-Path $sourceRoot 'configurations') -Filter '*.psd1' -File -Recurse | ForEach-Object {
    Write-Host "Loading PSD1 manifest: $($_.FullName)"
    $manifest = Import-PowerShellDataFile -LiteralPath $_.FullName
    if ([string]::IsNullOrWhiteSpace([string]$manifest.Name) -or
        [string]::IsNullOrWhiteSpace([string]$manifest.Configuration) -or
        [string]::IsNullOrWhiteSpace([string]$manifest.Module)) {
        throw "Manifest must define Name, Configuration, and Module: $($_.FullName)"
    }
    [pscustomobject]@{
        Name = [string]$manifest.Name
        ConfigurationName = if ($manifest.ConfigurationName) { [string]$manifest.ConfigurationName } else { [string]$manifest.Name }
        Priority = if ($null -ne $manifest.Priority) { [int]$manifest.Priority } else { 100 }
        Configuration = [string]$manifest.Configuration
        Module = [string]$manifest.Module
        RequiredModules = @($manifest.RequiredModules)
        Parameters = if ($manifest.Parameters) { $manifest.Parameters } else { @{} }
        Match = if ($manifest.Match) { $manifest.Match } else { @{} }
        ManifestPath = $_.FullName
    }
})

$configurationNames = @($requestedNames | Where-Object { -not [string]::IsNullOrWhiteSpace([string]$_) } | Select-Object -Unique)
if ($hasNodeContext -and $configurationNames.Count -eq 0 -and -not [string]::IsNullOrWhiteSpace($nodeName)) {
    $configurationNames = @($nodeName)
}
if ($configurationNames.Count -eq 0) {
    $configurationNames = @($manifests.ConfigurationName | Select-Object -Unique)
}

foreach ($configurationName in $configurationNames) {
    $selected = @($manifests | Where-Object {
        $_.ConfigurationName -eq $configurationName -and
        $(if ($hasNodeContext) { Test-ManifestMatch -Match $_.Match -Tags $nodeTags } else { $_.Match.Count -eq 0 })
    } | Sort-Object Priority, Name)
    if ($selected.Count -eq 0) {
        if ($hasNodeContext) {
            throw "No manifests match configuration '$configurationName' and the node tags."
        }
        continue
    }

    $configurationFunction = $selected[0].Configuration
    if (@($selected | Where-Object { $_.Configuration -ne $configurationFunction -or $_.Module -ne $selected[0].Module }).Count -gt 0) {
        throw "Manifests selected for '$configurationName' must use the same Configuration function and module."
    }
    $mergedParameters = @{}
    foreach ($item in $selected) {
        Merge-ConfigurationData -Target $mergedParameters -Overlay $item.Parameters
    }

    $modulePath = Join-Path $sourceRoot (Join-Path 'modules' $selected[0].Module)
    if (-not (Test-Path -LiteralPath $modulePath -PathType Leaf)) {
        throw "Configuration module not found: $modulePath"
    }
    foreach ($requiredModule in $selected.RequiredModules | Select-Object -Unique) {
        Write-Host "Loading required DSC module: $requiredModule"
        Import-Module -Name $requiredModule -ErrorAction Stop
    }
    Write-Host "Loading PSM1: $modulePath"
    Import-Module -Name $modulePath -Force -ErrorAction Stop
    $command = Get-Command -Name $configurationFunction -ErrorAction SilentlyContinue
    if (-not $command) {
        throw "Configuration function '$configurationFunction' was not loaded from the selected modules"
    }
    if (-not [string]::IsNullOrWhiteSpace($nodeName) -and $mergedParameters.ContainsKey('NodeName')) {
        $mergedParameters['NodeName'] = $nodeName
    }
    $targetPath = Join-Path $outputRoot $configurationName
    New-Item -ItemType Directory -Path $targetPath -Force | Out-Null
    & $command -OutputPath $targetPath @mergedParameters
    $generatedMofs = @(Get-ChildItem -LiteralPath $targetPath -Filter '*.mof' -File -Recurse)
    if ($generatedMofs.Count -ne 1) {
        throw "Configuration '$configurationName' must generate exactly one MOF; found $($generatedMofs.Count)"
    }
    Move-Item -LiteralPath $generatedMofs[0].FullName -Destination (Join-Path $outputRoot ($configurationName + '.mof')) -Force
}