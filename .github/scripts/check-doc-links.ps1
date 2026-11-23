# Checks that every relative Markdown link in the repository resolves to an
# existing file. Exits non-zero if any broken link is found, so it can be used
# as a CI guard against documentation regressions.
#
# Usage:
#   pwsh ./scripts/check-doc-links.ps1 [-Root <repo-root>]
#
# Notes:
#   - http(s)/mailto/anchors-only links are skipped.
#   - Link destinations are URL-decoded (GitHub renders "%5BMS-DSCPM%5D.pdf"
#     as "[MS-DSCPM].pdf", so the destination is encoded in the Markdown).
#   - A destination that starts with "/" is treated as repo-root-relative.

param(
    [string]$Root = (Get-Location).Path
)

$broken = @()
$files = Get-ChildItem -Path $Root -Recurse -Filter *.md |
    Where-Object { $_.FullName -notmatch '(\\|/)\.git(\\|/)' -and $_.FullName -notmatch '(\\|/)\.gotmp(\\|/)' }

foreach ($file in $files) {
    $dir = Split-Path -Parent $file.FullName
    $content = Get-Content -Raw $file.FullName
    $matches = [regex]::Matches($content, '\]\(([^)]+)\)')
    foreach ($m in $matches) {
        $link = $m.Groups[1].Value
        if ($link -match '^(https?://|mailto:|#|data:)') { continue }

        # Split off any anchor and strip inline-code backticks.
        $dest = ($link -split '#')[0] -replace '`', ''
        $dest = $dest.Trim()
        if ($dest -eq '') { continue }

        # URL-decode (e.g. %5BMS-DSCPM%5D.pdf -> [MS-DSCPM].pdf).
        try {
            $dest = [System.Uri]::UnescapeDataString($dest)
        } catch {
            # leave as-is if it is not valid percent-encoding
        }

        if ($dest.StartsWith('/')) {
            $target = Join-Path $Root $dest.TrimStart('/')
        } else {
            $target = Join-Path $dir $dest
        }

        if (-not (Test-Path -LiteralPath $target)) {
            $rel = $file.FullName.Substring($Root.TrimEnd('\','/').Length + 1)
            $broken += ("{0}  ->  {1}" -f $rel, $link)
        }
    }
}

if ($broken.Count -gt 0) {
    Write-Output ("FAIL: {0} broken relative link(s) found:" -f $broken.Count)
    $broken | Sort-Object -Unique | ForEach-Object { Write-Output "  $_" }
    exit 1
}

Write-Output "OK: no broken relative links found ($($files.Count) Markdown files checked)."
exit 0
