@echo off
REM 注意：请将此文件保存为 ANSI (GBK) 编码，否则中文可能会乱码或导致 'else' 等语法错误
REM chcp 65001 >nul
echo ========================================
echo         AI Summary Tool
echo ========================================
echo.

REM 检查是否已安装Go
go version >nul 2>&1
if errorlevel 1 (
    echo [错误] 未找到Go，请先安装Go 1.18+
    echo 下载地址：https://go.dev/dl/
    pause
    exit /b 1
)
echo  GO environment detected

REM 检查是否安装FFmpeg
ffmpeg -version >nul 2>&1
if errorlevel 1 (
    echo [警告] 未找到FFmpeg，视频处理功能将无法使用
    echo 下载地址：https://ffmpeg.org/download.html
    echo.
    set /p continue="是否继续启动？(y/n): "
    if /i "%continue%" neq "y" exit /b 1
) else (
    echo  FFmpeg environment detected 
)

REM 检查下载目录
if not exist "D:\download" (
    echo [提示] 创建下载目录: D:\download
    mkdir "D:\download"
)

echo.
echo [info] starting server...
echo.

REM 启动服务
go run main.go -mode server -port 8080

if errorlevel 1 (
    echo.
    echo [错误] 服务启动失败
    echo 请检查：
    echo  1. 端口8080是否被占用
    echo  2. 当前目录是否正确
    echo  3. 是否已运行 go mod tidy
    pause
    exit /b 1
)

pause
