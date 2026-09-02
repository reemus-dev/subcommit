param(
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA "Programs\subcommit\bin")
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$name = "subcommit"
$repo = "reemus-dev/subcommit"
$downloadBaseUrl = "https://github.com/$repo/releases/latest/download"

$processorArchitecture = if ($env:PROCESSOR_ARCHITEW6432) {
    $env:PROCESSOR_ARCHITEW6432
} else {
    $env:PROCESSOR_ARCHITECTURE
}

$arch = switch ($processorArchitecture.ToUpperInvariant()) {
    "AMD64" { "x86_64" }
    "ARM64" { "arm64" }
    default { throw "Unsupported architecture: $processorArchitecture" }
}

$tempDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Path $tempDir | Out-Null

try {
    $checksumsPath = Join-Path $tempDir "checksums.txt"
    Write-Host "Downloading checksums"
    Invoke-WebRequest -UseBasicParsing -Uri "$downloadBaseUrl/checksums.txt" -OutFile $checksumsPath

    $archivePattern = '^([0-9a-fA-F]{64})\s+(.+_Windows_' + [regex]::Escape($arch) + '\.zip)$'
    $releaseEntries = @(Get-Content -LiteralPath $checksumsPath | ForEach-Object {
        if ($_ -match $archivePattern) {
            [pscustomobject]@{
                Checksum = $Matches[1]
                Archive = $Matches[2]
            }
        }
    })

    if ($releaseEntries.Count -eq 0) {
        throw "No release archive found for Windows/$arch"
    }
    if ($releaseEntries.Count -gt 1) {
        throw "Multiple release archives found for Windows/$arch"
    }

    $release = $releaseEntries[0]
    $archivePath = Join-Path $tempDir $release.Archive
    Write-Host "Downloading $($release.Archive)"
    Invoke-WebRequest -UseBasicParsing -Uri "$downloadBaseUrl/$($release.Archive)" -OutFile $archivePath

    $actualChecksum = (Get-FileHash -Algorithm SHA256 -LiteralPath $archivePath).Hash
    if (-not $actualChecksum.Equals($release.Checksum, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "Checksum verification failed for $($release.Archive)"
    }

    $extractDir = Join-Path $tempDir "release"
    Expand-Archive -LiteralPath $archivePath -DestinationPath $extractDir
    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
    $InstallDir = (Resolve-Path -LiteralPath $InstallDir).Path

    Write-Host "Installing subcommit.exe and git-subcommit.exe in $InstallDir"
    Copy-Item -Force -LiteralPath (Join-Path $extractDir "subcommit.exe") -Destination $InstallDir
    Copy-Item -Force -LiteralPath (Join-Path $extractDir "git-subcommit.exe") -Destination $InstallDir

    $userPath = [System.Environment]::GetEnvironmentVariable("Path", [System.EnvironmentVariableTarget]::User)
    $userPathEntries = @($userPath -split ";" | Where-Object { $_ })
    if ($userPathEntries -notcontains $InstallDir) {
        $updatedUserPath = if ([string]::IsNullOrWhiteSpace($userPath)) {
            $InstallDir
        } else {
            $userPath.TrimEnd(";") + ";" + $InstallDir
        }
        [System.Environment]::SetEnvironmentVariable(
            "Path",
            $updatedUserPath,
            [System.EnvironmentVariableTarget]::User
        )
        Write-Host "Added $InstallDir to your user PATH"
    }

    if (($env:Path -split [System.IO.Path]::PathSeparator) -notcontains $InstallDir) {
        $env:Path += [System.IO.Path]::PathSeparator + $InstallDir
    }

    Write-Host "Installed successfully"
    Write-Host "Open a new terminal, then run subcommit --help."
} finally {
    Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $tempDir
}
