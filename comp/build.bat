@echo off
rem Build comp.exe tu source. Can cai Go: https://go.dev/dl
cd /d "%~dp0"
where go >nul 2>nul || (echo Chua cai Go. Tai o https://go.dev/dl roi chay lai. & pause & exit /b 1)
echo Dang chay test...
go test ./... || goto fail
echo Dang build comp.exe...
set GOOS=windows
set GOARCH=amd64
go build -trimpath -ldflags "-s -w" -o comp.exe . || goto fail
echo Xong: %~dp0comp.exe
pause
exit /b 0
:fail
echo Build loi, xem thong bao o tren.
pause
exit /b 1
