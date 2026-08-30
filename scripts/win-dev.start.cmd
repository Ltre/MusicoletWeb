@echo off
setlocal EnableExtensions
set "EXIT_CODE=1"
set "PUSHD_OK=0"

rem Development proxy settings copied from the provided FmlySys launcher pattern.
set "HTTP_PROXY=http://127.0.0.1:58591"
set "HTTPS_PROXY=http://127.0.0.1:58591"
set "ALL_PROXY=socks5://127.0.0.1:51837"
set "http_proxy=%HTTP_PROXY%"
set "https_proxy=%HTTPS_PROXY%"
set "all_proxy=%ALL_PROXY%"
set "NO_PROXY=127.0.0.1,localhost"
set "no_proxy=%NO_PROXY%"

if not defined MUSICOLET_BIND_HOST set "MUSICOLET_BIND_HOST=0.0.0.0"
if not defined MUSICOLET_PORT set "MUSICOLET_PORT=4001"
set "SCRIPT_DIR=%~dp0"
for %%I in ("%SCRIPT_DIR%..") do set "REPO_ROOT=%%~fI"
if not exist "%REPO_ROOT%\go.mod" goto :repo_failed
pushd "%REPO_ROOT%" >nul
if errorlevel 1 goto :pushd_failed
set "PUSHD_OK=1"
if not defined MUSICOLET_DATA_DIR set "MUSICOLET_DATA_DIR=%REPO_ROOT%\data"
set "CONFIG_FILE=%MUSICOLET_DATA_DIR%\config.env"
if not exist "%MUSICOLET_DATA_DIR%" mkdir "%MUSICOLET_DATA_DIR%" >nul 2>&1
if not exist "%REPO_ROOT%\bin" mkdir "%REPO_ROOT%\bin" >nul 2>&1
if not exist "%CONFIG_FILE%" (
  >"%CONFIG_FILE%" echo # MusicoletWeb private runtime configuration. data/ is ignored by Git.
  >>"%CONFIG_FILE%" echo MUSICOLET_ADMIN_USERNAME=admin
  >>"%CONFIG_FILE%" echo MUSICOLET_ADMIN_PASSWORD=
  >>"%CONFIG_FILE%" echo MUSICOLET_ADMIN_TOTP_SECRET=
  >>"%CONFIG_FILE%" echo MUSICOLET_SESSION_KEY=
  >>"%CONFIG_FILE%" echo MUSICOLET_MASTER_KEY=
  >>"%CONFIG_FILE%" echo # MUSICOLET_AGENT_TOKEN is bootstrap-only after first successful startup.
  >>"%CONFIG_FILE%" echo MUSICOLET_AGENT_TOKEN=
  >>"%CONFIG_FILE%" echo MUSICOLET_PUBLIC_BASE_URL=
  echo [MusicoletWeb] Created config template: %CONFIG_FILE%
)
findstr /b /c:"MUSICOLET_MASTER_KEY=" "%CONFIG_FILE%" >nul 2>&1 || >>"%CONFIG_FILE%" echo MUSICOLET_MASTER_KEY=
findstr /b /c:"MUSICOLET_AGENT_TOKEN=" "%CONFIG_FILE%" >nul 2>&1 || >>"%CONFIG_FILE%" echo MUSICOLET_AGENT_TOKEN=
for /f "usebackq eol=# tokens=1,* delims==" %%A in ("%CONFIG_FILE%") do if not "%%A"=="" set "%%A=%%B"
where go >nul 2>&1
if errorlevel 1 goto :go_missing
where git >nul 2>&1
if errorlevel 1 goto :git_missing

echo [MusicoletWeb] Repository: %REPO_ROOT%
echo [MusicoletWeb] HTTP_PROXY=%HTTP_PROXY%
echo [MusicoletWeb] HTTPS_PROXY=%HTTPS_PROXY%
echo [MusicoletWeb] ALL_PROXY=%ALL_PROXY%
echo [MusicoletWeb] Port: %MUSICOLET_PORT%
echo [MusicoletWeb] Local URL: http://localhost:%MUSICOLET_PORT%/
echo [MusicoletWeb] Data: %MUSICOLET_DATA_DIR%
echo.
echo [MusicoletWeb] Tidying Go modules...
go mod tidy
if errorlevel 1 goto :modules_failed
echo [MusicoletWeb] Downloading dependencies...
go mod download all
if errorlevel 1 goto :modules_failed
go mod verify
if errorlevel 1 goto :modules_failed
echo [MusicoletWeb] Rebuilding server...
go build -trimpath -o "%REPO_ROOT%\bin\musicoletweb.exe" .\cmd\server
if errorlevel 1 goto :build_failed
echo [MusicoletWeb] Starting...
"%REPO_ROOT%\bin\musicoletweb.exe"
set "EXIT_CODE=%ERRORLEVEL%"
if "%EXIT_CODE%"=="0" goto :success
goto :fail

:repo_failed
echo [MusicoletWeb] Repository root could not be resolved from %SCRIPT_DIR%
goto :fail
:pushd_failed
echo [MusicoletWeb] Failed to enter %REPO_ROOT%
goto :fail
:go_missing
echo [MusicoletWeb] Go was not found in PATH.
goto :fail
:git_missing
echo [MusicoletWeb] Git was not found in PATH.
goto :fail
:modules_failed
echo [MusicoletWeb] Go module preparation failed. Check local proxy/DNS settings.
goto :fail
:build_failed
echo [MusicoletWeb] Go build failed.
goto :fail
:fail
if "%PUSHD_OK%"=="1" popd >nul
echo.
echo [MusicoletWeb] Startup failed. Press any key to close this window.
pause >nul
endlocal & exit /b %EXIT_CODE%
:success
if "%PUSHD_OK%"=="1" popd >nul
endlocal & exit /b 0
