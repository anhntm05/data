@echo off
rem Build insertgen.exe tu source. Can cai Go: https://go.dev/dl
cd /d "%~dp0"
where go >nul 2>nul || (echo Chua cai Go. Tai o https://go.dev/dl roi chay lai. & pause & exit /b 1)
echo Dang chay test...
go test ./... || goto fail
echo Dang build insertgen.exe...
set GOOS=windows
set GOARCH=amd64
go build -trimpath -ldflags "-s -w" -o insertgen.exe . || goto fail
echo Xong: %~dp0insertgen.exe
pause
exit /b 0
:fail
echo Build loi, xem thong bao o tren.
pause
exit /b 1
