@echo off
chcp 65001 >nul
title 视频字幕生成与AI总结工具

echo ╔════════════════════════════════════════════════════════════╗
echo ║   视频字幕生成与AI总结工具 - 快速启动                      ║
echo ║   Power by FFmpeg + B站ASR + AI总结                        ║
echo ╚════════════════════════════════════════════════════════════╝
echo.

:: 检查Go
where go >nul 2>nul
if %errorlevel% neq 0 (
    echo ❌ 未找到Go，请安装Go并添加到PATH
    pause
    exit /b 1
)
echo ✅ Go环境正常

:: 检查FFmpeg
where ffmpeg >nul 2>nul
if %errorlevel% neq 0 (
    echo ❌ 未找到FFmpeg
    echo    请下载并安装FFmpeg: https://ffmpeg.org/download.html
    echo    然后将bin目录添加到系统PATH
    pause
    exit /b 1
)
echo ✅ FFmpeg环境正常

echo.
echo 请选择运行模式：
echo.
echo   1. 启动HTTP服务 (推荐)
echo      - 提供Web界面，支持视频处理和AI总结
echo      - 访问 http://localhost:8080
echo.
echo   2. 命令行处理视频
echo      - 直接处理视频，生成字幕和截图
echo      - 需要输入视频路径
echo.
echo   3. 运行环境检测
echo      - 检查依赖和配置
echo.
set /p choice="请输入选择 (1/2/3): "

if "%choice%"=="1" (
    echo.
    echo 🚀 启动HTTP服务...
    echo    访问地址: http://localhost:8080
    echo    按 Ctrl+C 停止服务
    echo.
    go run main_enhanced.go -mode server -port 8080
) else if "%choice%"=="2" (
    echo.
    set /p video="请输入视频文件路径: "
    if "!video!"=="" (
        echo ❌ 未输入视频路径
        pause
        exit /b 1
    )
    echo 🚀 处理视频: !video!
    go run main_enhanced.go -mode cli -video "!video!"
) else if "%choice%"=="3" (
    echo.
    go run test_tool.go
) else (
    echo ❌ 无效选择
)

echo.
pause
