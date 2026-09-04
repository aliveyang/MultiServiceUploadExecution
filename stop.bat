@echo off
chcp 65001 >nul
setlocal enabledelayedexpansion

title 停止多服务器部署工具

set "APP_DIR=%~dp0"
cd /d "%APP_DIR%"

set "EXE_NAME=deploy.exe"
set "PID_FILE=%APP_DIR%deploy.pid"

echo ====================================================
echo   多服务器部署工具 [Multi-Service Deployer] - 停止
echo ====================================================

set "STOPPED=0"

REM 1. 优先根据 PID 文件精准终止对应进程
if exist "%PID_FILE%" (
    set /p TARGET_PID=<"%PID_FILE%"
    if defined TARGET_PID (
        tasklist /FI "PID eq !TARGET_PID!" 2>nul | findstr /I "!TARGET_PID!" >nul
        if !errorlevel! equ 0 (
            echo 正在停止进程 [PID: !TARGET_PID!]...
            taskkill /F /PID !TARGET_PID! >nul 2>&1
            set "STOPPED=1"
        )
    )
    del /f /q "%PID_FILE%" >nul 2>&1
)

REM 2. 兜底扫描并终止遗留的 deploy.exe 进程
tasklist /FI "IMAGENAME eq %EXE_NAME%" 2>nul | findstr /I "%EXE_NAME%" >nul
if %errorlevel% equ 0 (
    echo 正在清理剩余的 %EXE_NAME% 进程...
    taskkill /F /IM %EXE_NAME% >nul 2>&1
    set "STOPPED=1"
)

if !STOPPED! equ 1 (
    echo [成功] 多服务器部署工具已安全停止。
) else (
    echo [提示] 未检测到正在运行的 %EXE_NAME% 服务。
)

echo.
ping 127.0.0.1 -n 2 >nul
exit /b 0
