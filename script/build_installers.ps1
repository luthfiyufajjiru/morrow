# build_installers.ps1
$platforms = @("x86", "x64", "arm64")

foreach ($platform in $platforms) {
    Write-Host "Building installer for $platform..." -ForegroundColor Cyan
    dotnet build -p:Platform=$platform
    if ($LASTEXITCODE -ne 0) {
        Write-Host "Failed to build $platform" -ForegroundColor Red
        exit $LASTEXITCODE
    }
}

Write-Host "All installers built successfully!" -ForegroundColor Green
