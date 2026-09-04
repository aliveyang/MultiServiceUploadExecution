@echo off
chcp 65001 >nul
setlocal enabledelayedexpansion

title 启动多服务器部署工具 [后台服务]

set "APP_DIR=%~dp0"
cd /d "%APP_DIR%"

set "EXE_NAME=deploy.exe"
set "LOG_FILE=%APP_DIR%deploy.log"
set "PID_FILE=%APP_DIR%deploy.pid"

echo ====================================================
echo   多服务器部署工具 [Multi-Service Deployer] - 启动
echo ====================================================

REM 1. 检查 deploy.exe 是否存在
if not exist "%APP_DIR%%EXE_NAME%" (
    echo [错误] 未在当前目录下找到 %EXE_NAME%，请确认可执行文件存在。
    echo 按任意键退出...
    pause >nul
    exit /b 1
)

REM 2. 检查是否已经在运行
if exist "%PID_FILE%" (
    set /p OLD_PID=<"%PID_FILE%"
    if defined OLD_PID (
        tasklist /FI "PID eq !OLD_PID!" 2>nul | findstr /I "!OLD_PID!" >nul
        if !errorlevel! equ 0 (
            echo [提示] 服务已在后台运行中 [PID: !OLD_PID!]。
            echo 访问地址: http://127.0.0.1:8080
            echo 如需重启，请先双击运行 stop.bat 停止服务。
            goto :END
        )
    )
    del /f /q "%PID_FILE%" >nul 2>&1
)

tasklist /FI "IMAGENAME eq %EXE_NAME%" 2>nul | findstr /I "%EXE_NAME%" >nul
if %errorlevel% equ 0 (
    echo [提示] 检测到已存在运行中的 %EXE_NAME% 进程。
    echo 访问地址: http://127.0.0.1:8080
    echo 如需重启，请先双击运行 stop.bat。
    goto :END
)

REM 3. 通过 PowerShell 在后台静默启动，日志重定向至 deploy.log
echo 正在后台静默启动 Web 控制台...
powershell -NoProfile -ExecutionPolicy Bypass -Command ^
    "$p = Start-Process -FilePath '%APP_DIR%%EXE_NAME%' -ArgumentList '-web' -WorkingDirectory '%APP_DIR%' -RedirectStandardOutput '%LOG_FILE%' -RedirectStandardError '%APP_DIR%deploy_err.log' -WindowStyle Hidden -PassThru; if ($p) { $p.Id | Out-File -FilePath '%PID_FILE%' -Encoding ascii }"

REM 等待 2 秒确认进程启动状态
ping 127.0.0.1 -n 3 >nul

if exist "%PID_FILE%" (
    set /p NEW_PID=<"%PID_FILE%"
    tasklist /FI "PID eq !NEW_PID!" 2>nul | findstr /I "!NEW_PID!" >nul
    if !errorlevel! equ 0 (
        echo [成功] 服务已成功在后台启动！
        echo ----------------------------------------------------
        echo   进程 PID  : !NEW_PID!
        echo   访问地址  : http://127.0.0.1:8080
        echo   日志文件  : %LOG_FILE%
        echo   停止脚本  : stop.bat
        echo ----------------------------------------------------
        echo 提示: 即将在默认浏览器中打开控制台...
        start http://127.0.0.1:8080
        goto :END
    )
)

echo [错误] 服务未能成功启动，请查看日志: %LOG_FILE%
if exist "%APP_DIR%deploy_err.log" (
    echo ----- 错误输出 -----
    type "%APP_DIR%deploy_err.log"
    echo --------------------
)

:END
echo.
ping 127.0.0.1 -n 3 >nul
exit /b 0
