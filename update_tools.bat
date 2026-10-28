@echo off
rem Tai ban moi nhat cua comp.exe va insertgen.exe (GitHub build tu dong)
rem ve cung thu muc voi file nay.
setlocal
set "DIR=%~dp0"
set "BASE=https://github.com/anhntm05/data/releases/download/latest"
set FAIL=0
for %%T in (comp insertgen) do call :get %%T
echo.
if %FAIL%==0 (echo Xong. Cac file nam trong %DIR%) else (echo Co file tai khong duoc, xem thong bao o tren.)
if exist "%DIR%uatdiff.exe" echo Luu y: uatdiff.exe cu da doi ten thanh comp.exe, ban co the xoa uatdiff.exe.
pause
exit /b %FAIL%

:get
echo Dang tai %1.exe ...
curl.exe -fsSL --retry 2 -o "%DIR%%1.exe.new" "%BASE%/%1.exe"
if errorlevel 1 (
  echo   Tai that bai: %BASE%/%1.exe
  del "%DIR%%1.exe.new" 2>nul
  set FAIL=1
  exit /b
)
move /y "%DIR%%1.exe.new" "%DIR%%1.exe" >nul
if errorlevel 1 (
  echo   Khong ghi de duoc %1.exe. Hay dong tool dang chay roi chay lai file nay.
  del "%DIR%%1.exe.new" 2>nul
  set FAIL=1
  exit /b
)
echo   OK: %1.exe
exit /b
