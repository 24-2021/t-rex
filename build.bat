@echo off
setlocal
echo Building T-REX...
echo.

REM Check whether Go is available.
set "GO_CMD=go"
where go >nul 2>nul
if %ERRORLEVEL% neq 0 (
    if exist "%USERPROFILE%\sdk\go1.20.14\bin\go.exe" (
        set "GO_CMD=%USERPROFILE%\sdk\go1.20.14\bin\go.exe"
    ) else (
        echo Error: Go is not installed or not in PATH
        pause
        exit /b 1
    )
)

REM Set version information.
if "%VERSION%"=="" set VERSION=dev
for /f %%I in ('powershell -NoProfile -Command "Get-Date -Format yyyy-MM-dd_HH-mm-ss"') do set "BUILD_TIME=%%I"

REM Set build options.
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0
set "LDFLAGS=-s -w -X main.version=%VERSION% -X main.buildTime=%BUILD_TIME%"

echo Version: %VERSION%
echo Target: %GOOS%/%GOARCH%
echo.

REM Create output directory.
if not exist "dist" mkdir dist

REM Build.
"%GO_CMD%" build -buildvcs=false -ldflags="%LDFLAGS%" -o "dist/t-rex.exe" .

if %ERRORLEVEL% equ 0 (
    echo.
    echo Build successful! Output: dist\t-rex.exe
    echo.
    REM Show output file size.
    for %%A in ("dist\t-rex.exe") do echo File size: %%~zA bytes
) else (
    echo.
    echo Build failed!
    pause
    exit /b 1
)
