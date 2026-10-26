@echo off
rem Tai uatdiff.exe ban moi nhat (GitHub build tu dong) ve cung thu muc voi file nay.
setlocal
set "DIR=%~dp0"
set "URL=https://github.com/anhntm05/data/releases/download/latest/uatdiff.exe"
echo Dang tai ban moi nhat...
curl.exe -fL --retry 2 -o "%DIR%uatdiff.exe.new" "%URL%"
if errorlevel 1 (
  echo Tai that bai. Kiem tra mang hoac mo link: %URL%
  del "%DIR%uatdiff.exe.new" 2>nul
  pause
  exit /b 1
)
move /y "%DIR%uatdiff.exe.new" "%DIR%uatdiff.exe" >nul
if errorlevel 1 (
  echo Khong ghi de duoc uatdiff.exe. Hay dong tool dang chay roi chay lai file nay.
  pause
  exit /b 1
)
echo Xong: %DIR%uatdiff.exe
pause
