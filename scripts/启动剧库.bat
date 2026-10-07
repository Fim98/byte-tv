@echo off
setlocal
set "GOTOOLCHAIN=local"
chcp 65001 >nul
title juku 剧库
cd /d "%~dp0.."
where go >nul 2>nul
if not errorlevel 1 (
  go env -w GOPROXY=https://goproxy.cn,direct
  if errorlevel 1 exit /b 1
  go env -w GOSUMDB=off
  if errorlevel 1 exit /b 1
)
if not exist "dist\juku_windows_amd64.exe" (
  echo 找不到下载器，请先编译或获取包含 dist 的完整版本。
  pause
  exit /b 1
)
"dist\juku_windows_amd64.exe" %*
if errorlevel 1 pause
