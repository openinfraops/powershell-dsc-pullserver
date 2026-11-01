@{
    Name = 'example-node-base'
    ConfigurationName = 'NodeDefinition'
    Priority = 10
    Match = @{}
    Module = 'ExampleConfigurations.psm1'
    RequiredModules = @('cChoco')
    Configuration = 'default'
    Parameters = @{
        NodeName = 'localhost'
        Configurations = @{
            'default::Sources' = @(
                @{ Name = 'Boursochoco'; Ensure = 'Present'; Location = '\\fileserver\Chocolatey'; Priority = 0 }
            )
            'default::Packages' = @(
                @{ Name = '7-zip'; Ensure = 'Present' }
                @{ Name = 'notepadplusplus'; Ensure = 'Present'; AutoUpgrade = $true }
                @{ Name = 'old-package'; Ensure = 'Absent' }
                @{ Name = 'qualyscloudagent'; Ensure = 'Present'; Version = '4.21.0' }
            )
            'default::WindowsFeature' = @(
                @{ Name = 'AD-Certificate'; Ensure = 'Present' }
                @{ Name = 'ADCS-Cert-Authority'; Ensure = 'Present'; DependsOn = '[WindowsFeature]AD-Certificate' }
                @{ Name = 'ADCS-Enroll-Web-Svc'; Ensure = 'Present'; DependsOn = '[WindowsFeature]AD-Certificate' }
                @{ Name = 'ADCS-Web-Enrollment'; Ensure = 'Present'; DependsOn = '[WindowsFeature]AD-Certificate' }
                @{ Name = 'RSAT-ADCS'; Ensure = 'Present'; DependsOn = '[WindowsFeature]AD-Certificate' }
                @{ Name = 'RSAT-ADCS-Mgmt'; Ensure = 'Present'; DependsOn = '[WindowsFeature]AD-Certificate' }
            )
        }
    }
}
