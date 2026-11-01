Configuration default {
    param(
        [Parameter(Mandatory = $true)][string]$NodeName,
        [Parameter(Mandatory = $true)][System.Collections.IDictionary]$Configurations
    )

    Import-DscResource -ModuleName PSDesiredStateConfiguration
    Import-DscResource -ModuleName cChoco

    Node $NodeName {
        $sources = $Configurations['default::Sources']
        if ($sources) {
            foreach ($key in $sources.Keys) {
                $source = $sources[$key]
                cChocoSource $key {
                    Name = [string]$source.Name
                    Ensure = [string]$source.Ensure
                    Source = [string]$source.Location
                    Priority = [int]$source.Priority
                }
            }
        }

        $packages = $Configurations['default::Packages']
        if ($packages) {
            foreach ($key in $packages.Keys) {
                $package = $packages[$key]
                if ($package.Ensure -eq 'Absent') {
                    cChocoPackageInstaller $key {
                        Name = [string]$package.Name
                        Ensure = [string]$package.Ensure
                    }
                } elseif ($package.AutoUpgrade) {
                    cChocoPackageInstaller $key {
                        Name = [string]$package.Name
                        Ensure = [string]$package.Ensure
                        AutoUpgrade = [bool]$package.AutoUpgrade
                    }
                } else {
                    cChocoPackageInstaller $key {
                        Name = [string]$package.Name
                        Ensure = [string]$package.Ensure
                        MinimumVersion = if ($package.Contains('Version')) { [string]$package.Version } else { $null }
                    }
                }
            }
        }

        $features = $Configurations['default::WindowsFeature']
        if ($features) {
            foreach ($key in $features.Keys) {
                $feature = $features[$key]
                if ([string]::IsNullOrWhiteSpace([string]$feature.DependsOn)) {
                    WindowsFeature $key {
                        Name = [string]$feature.Name
                        Ensure = [string]$feature.Ensure
                    }
                } else {
                    WindowsFeature $key {
                        Name = [string]$feature.Name
                        Ensure = [string]$feature.Ensure
                        DependsOn = [string]$feature.DependsOn
                    }
                }
            }
        }

        $environmentVariables = $Configurations['default::EnvironmentVariable']
        if ($environmentVariables) {
            foreach ($key in $environmentVariables.Keys) {
                $variable = $environmentVariables[$key]
                Environment $key {
                    Name = [string]$variable.Name
                    Ensure = [string]$variable.Ensure
                    Value = [string]$variable.Value
                }
            }
        }

        $registryEntries = $Configurations['default::Registry']
        if ($registryEntries) {
            foreach ($key in $registryEntries.Keys) {
                $registry = $registryEntries[$key]
                Registry $key {
                    Key = [string]$registry.Key
                    ValueName = [string]$registry.ValueName
                    ValueData = [string]$registry.ValueData
                    ValueType = [string]$registry.ValueType
                    Ensure = [string]$registry.Ensure
                }
            }
        }
    }
}

Configuration defaultWindowsFeatures {
    param([string]$NodeName, [System.Collections.IDictionary]$Resources)
    Import-DscResource -ModuleName PSDesiredStateConfiguration
    Node $NodeName {
        foreach ($key in $Resources.Keys) {
            $feature = $Resources[$key]
            if ([string]::IsNullOrWhiteSpace([string]$feature.DependsOn)) {
                WindowsFeature $key {
                    Name = [string]$feature.Name
                    Ensure = [string]$feature.Ensure
                }
            } else {
                WindowsFeature $key {
                    Name = [string]$feature.Name
                    Ensure = [string]$feature.Ensure
                    DependsOn = [string]$feature.DependsOn
                }
            }
        }
    }
}

Configuration defaultEnvironmentVariables {
    param([string]$NodeName, [System.Collections.IDictionary]$Resources)
    Import-DscResource -ModuleName PSDesiredStateConfiguration
    Node $NodeName {
        foreach ($key in $Resources.Keys) {
            $variable = $Resources[$key]
            Environment $key {
                Name = [string]$variable.Name
                Ensure = [string]$variable.Ensure
                Value = [string]$variable.Value
            }
        }
    }
}

Configuration defaultRegistry {
    param([string]$NodeName, [System.Collections.IDictionary]$Resources)
    Import-DscResource -ModuleName PSDesiredStateConfiguration
    Node $NodeName {
        foreach ($key in $Resources.Keys) {
            $registry = $Resources[$key]
            Registry $key {
                Key = [string]$registry.Key
                ValueName = [string]$registry.ValueName
                ValueData = [string]$registry.ValueData
                ValueType = [string]$registry.ValueType
                Ensure = [string]$registry.Ensure
            }
        }
    }
}

Configuration defaultSources {
    param([string]$NodeName, [System.Collections.IDictionary]$Resources)
    Import-DscResource -ModuleName cChoco
    Node $NodeName {
        foreach ($key in $Resources.Keys) {
            $source = $Resources[$key]
            cChocoSource $key {
                Name = [string]$source.Name
                Ensure = [string]$source.Ensure
                Source = [string]$source.Location
                Priority = [int]$source.Priority
            }
        }
    }
}

Configuration defaultPackages {
    param([string]$NodeName, [System.Collections.IDictionary]$Resources)
    Import-DscResource -ModuleName cChoco
    Node $NodeName {
        foreach ($key in $Resources.Keys) {
            $package = $Resources[$key]
            if ($package.Ensure -eq 'Absent') {
                cChocoPackageInstaller $key {
                    Name = [string]$package.Name
                    Ensure = [string]$package.Ensure
                }
            } elseif ($package.AutoUpgrade) {
                cChocoPackageInstaller $key {
                    Name = [string]$package.Name
                    Ensure = [string]$package.Ensure
                    AutoUpgrade = [bool]$package.AutoUpgrade
                }
            } else {
                cChocoPackageInstaller $key {
                    Name = [string]$package.Name
                    Ensure = [string]$package.Ensure
                    MinimumVersion = if ($package.Contains('Version')) { [string]$package.Version } else { $null }
                }
            }
        }
    }
}
