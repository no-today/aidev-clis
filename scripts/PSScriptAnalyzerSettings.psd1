# PSScriptAnalyzer settings for the Windows install scripts.
#   pwsh -Command "Invoke-ScriptAnalyzer -Path scripts\install.ps1 -Settings scripts\PSScriptAnalyzerSettings.psd1"
@{
    # PSAvoidUsingWriteHost: install.ps1 / uninstall.ps1 are interactive
    # installers whose Write-Host calls are intentional, colored, user-facing
    # progress. They are not library code and their output is never captured,
    # so writing to the host is the correct choice, not a defect.
    ExcludeRules = @('PSAvoidUsingWriteHost')
}
