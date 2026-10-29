@echo off
rem Build exe\comp_<yyyymmdd_hhmmss>.exe tu source. Can cai Go: https://go.dev/dl
cd /d "%~dp0"
where go >nul 2>nul || (echo Chua cai Go. Tai o https://go.dev/dl roi chay lai. & pause & exit /b 1)
for /f %%i in ('powershell -NoProfile -Command "Get-Date -Format yyyyMMdd_HHmmss"') do set "TS=%%i"
if not exist exe mkdir exe
set "OUT=exe\comp_%TS%.exe"
echo Dang chay test...
go test ./... || goto fail
echo Dang build %OUT%...
set GOOS=windows
set GOARCH=amd64
go build -trimpath -ldflags "-s -w" -o "%OUT%" . || goto fail
echo Xong: %~dp0%OUT%
pause
exit /b 0
:fail
echo Build loi, xem thong bao o tren.
pause
exit /b 1
