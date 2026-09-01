@echo off
setlocal EnableExtensions DisableDelayedExpansion

rem MusicoletWeb Windows development launcher.
rem Dependencies and both binaries are rebuilt before every start.

set "EXIT_CODE=1"
set "PUSHD_OK=0"

set "HTTP_PROXY=http://127.0.0.1:58591"
set "HTTPS_PROXY=http://127.0.0.1:58591"
set "ALL_PROXY=socks5://127.0.0.1:51837"
set "http_proxy=%HTTP_PROXY%"
set "https_proxy=%HTTPS_PROXY%"
set "all_proxy=%ALL_PROXY%"
set "NO_PROXY=127.0.0.1,localhost"
set "no_proxy=%NO_PROXY%"

set "MUSICOLET_BIND_HOST=0.0.0.0"
set "MUSICOLET_PORT=4001"
set "MUSICOLET_DEV_AUTH_ENABLED=1"

set "SCRIPT_DIR=%~dp0"
for %%I in ("%SCRIPT_DIR%..") do set "REPO_ROOT=%%~fI"
if not exist "%REPO_ROOT%\go.mod" goto :repo_failed

pushd "%REPO_ROOT%" >nul
if errorlevel 1 goto :pushd_failed
set "PUSHD_OK=1"
set "MUSICOLET_DATA_DIR=%REPO_ROOT%\data"
set "MUSICOLET_CONFIG_FILE=%MUSICOLET_DATA_DIR%\config.env"
set "MUSICOLET_BIN_DIR=%MUSICOLET_DATA_DIR%\bin"

if not exist "%MUSICOLET_DATA_DIR%" mkdir "%MUSICOLET_DATA_DIR%" >nul 2>&1
if not exist "%MUSICOLET_BIN_DIR%" mkdir "%MUSICOLET_BIN_DIR%" >nul 2>&1
if not exist "%MUSICOLET_CONFIG_FILE%" (
    >"%MUSICOLET_CONFIG_FILE%" echo # MusicoletWeb private runtime configuration. data/ is ignored by Git.
    >>"%MUSICOLET_CONFIG_FILE%" echo MUSICOLET_ADMIN_USERNAME=admin
    >>"%MUSICOLET_CONFIG_FILE%" echo MUSICOLET_ADMIN_PASSWORD=
    >>"%MUSICOLET_CONFIG_FILE%" echo MUSICOLET_ADMIN_TOTP_SECRET=
    >>"%MUSICOLET_CONFIG_FILE%" echo MUSICOLET_SESSION_KEY=
    >>"%MUSICOLET_CONFIG_FILE%" echo MUSICOLET_AGENT_TOKEN=
    >>"%MUSICOLET_CONFIG_FILE%" echo MUSICOLET_PUBLIC_BASE_URL=http://localhost:4001
    echo [MusicoletWeb] Created configuration template: %MUSICOLET_CONFIG_FILE%
    goto :configuration_created
)

for /f "usebackq eol=# tokens=1,* delims==" %%A in ("%MUSICOLET_CONFIG_FILE%") do set "%%A=%%B"
rem This launcher is intentionally development-only; config.env cannot turn production checks off elsewhere.
set "MUSICOLET_DEV_AUTH_ENABLED=1"

if not defined MUSICOLET_ADMIN_USERNAME goto :username_missing
if not defined MUSICOLET_ADMIN_PASSWORD goto :password_missing
if not defined MUSICOLET_SESSION_KEY goto :session_key_missing
if "%MUSICOLET_SESSION_KEY:~31,1%"=="" goto :session_key_short

where go >nul 2>&1
if errorlevel 1 goto :go_missing
where git >nul 2>&1
if errorlevel 1 goto :git_missing

echo [MusicoletWeb] Repository root: %REPO_ROOT%
echo [MusicoletWeb] HTTP_PROXY=%HTTP_PROXY%
echo [MusicoletWeb] HTTPS_PROXY=%HTTPS_PROXY%
echo [MusicoletWeb] ALL_PROXY=%ALL_PROXY%
echo [MusicoletWeb] Bind host: %MUSICOLET_BIND_HOST%
echo [MusicoletWeb] Port: %MUSICOLET_PORT%
echo [MusicoletWeb] Local URL: http://localhost:%MUSICOLET_PORT%/
echo [MusicoletWeb] LAN/domain URL: http://^<this-machine-ip-or-domain^>:%MUSICOLET_PORT%/
echo [MusicoletWeb] Data directory: %MUSICOLET_DATA_DIR%
echo [MusicoletWeb] Configuration: %MUSICOLET_CONFIG_FILE%
echo.

echo [MusicoletWeb] Tidying Go module metadata...
go mod tidy
if errorlevel 1 goto :tidy_failed
echo [MusicoletWeb] Downloading Go module dependencies...
go mod download all
if errorlevel 1 goto :download_failed
echo [MusicoletWeb] Verifying Go module dependencies...
go mod verify
if errorlevel 1 goto :verify_failed

echo [MusicoletWeb] Rebuilding Windows server...
go build -trimpath -o "%MUSICOLET_BIN_DIR%\musicoletweb.exe" ./cmd/musicoletweb
if errorlevel 1 goto :server_build_failed

echo [MusicoletWeb] Rebuilding Termux Linux arm64 agent...
set "GOOS=linux"
set "GOARCH=arm64"
set "CGO_ENABLED=0"
go build -trimpath -o "%MUSICOLET_BIN_DIR%\musicolet-agent-linux-arm64" ./cmd/musicolet-agent
set "AGENT_BUILD_EXIT=%ERRORLEVEL%"
set "GOOS="
set "GOARCH="
set "CGO_ENABLED="
if not "%AGENT_BUILD_EXIT%"=="0" goto :agent_build_failed

echo [MusicoletWeb] Starting server...
"%MUSICOLET_BIN_DIR%\musicoletweb.exe"
set "EXIT_CODE=%ERRORLEVEL%"
if "%EXIT_CODE%"=="0" goto :success
echo [MusicoletWeb] Server exited with code %EXIT_CODE%.
goto :fail

:repo_failed
echo [MusicoletWeb] Repository root could not be resolved from: %SCRIPT_DIR%
goto :fail
:pushd_failed
echo [MusicoletWeb] Failed to enter repository root: %REPO_ROOT%
goto :fail
:go_missing
echo [MusicoletWeb] Go was not found in PATH.
goto :fail
:git_missing
echo [MusicoletWeb] Git was not found in PATH.
goto :fail
:tidy_failed
echo [MusicoletWeb] go mod tidy failed. Check the configured local proxy.
goto :fail
:download_failed
echo [MusicoletWeb] Dependency download failed. Check the configured local proxy.
goto :fail
:verify_failed
echo [MusicoletWeb] Go module verification failed.
goto :fail
:server_build_failed
echo [MusicoletWeb] Windows server build failed.
goto :fail
:agent_build_failed
echo [MusicoletWeb] Termux arm64 agent build failed.
goto :fail

:configuration_created
echo [MusicoletWeb] Startup paused before dependency download or compilation.
echo [MusicoletWeb] Edit the private file and set:
echo [MusicoletWeb]   MUSICOLET_ADMIN_USERNAME
echo [MusicoletWeb]   MUSICOLET_ADMIN_PASSWORD
echo [MusicoletWeb]   MUSICOLET_SESSION_KEY ^(at least 32 characters^)
echo [MusicoletWeb] TOTP and Agent token may be empty only because this Windows launcher enables development auth.
goto :configuration_required
:username_missing
echo [MusicoletWeb] MUSICOLET_ADMIN_USERNAME is empty in: %MUSICOLET_CONFIG_FILE%
goto :configuration_required
:password_missing
echo [MusicoletWeb] MUSICOLET_ADMIN_PASSWORD is empty in: %MUSICOLET_CONFIG_FILE%
goto :configuration_required
:session_key_missing
echo [MusicoletWeb] MUSICOLET_SESSION_KEY is empty in: %MUSICOLET_CONFIG_FILE%
goto :configuration_required
:session_key_short
echo [MusicoletWeb] MUSICOLET_SESSION_KEY must contain at least 32 characters: %MUSICOLET_CONFIG_FILE%
goto :configuration_required
:configuration_required
if "%PUSHD_OK%"=="1" popd >nul
echo [MusicoletWeb] Fill the configuration and run scripts\win-dev.start.cmd again.
endlocal & exit /b 2

:fail
if "%PUSHD_OK%"=="1" popd >nul
echo.
echo [MusicoletWeb] Startup failed. Press any key to close this window.
pause >nul
endlocal & exit /b %EXIT_CODE%

:success
if "%PUSHD_OK%"=="1" popd >nul
endlocal & exit /b 0
