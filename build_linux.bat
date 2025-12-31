:: filepath: d:\project\ccode\build_linux.bat
@echo off
echo ==========================================
echo      Building for Linux (AMD64)...
echo ==========================================

:: 设置交叉编译环境变量
set GOOS=linux
set GOARCH=amd64

:: 编译
go build -o video-ai-helper-linux main.go

if %ERRORLEVEL% NEQ 0 (
    echo Build failed!
    pause
    exit /b %ERRORLEVEL%
)

echo.
echo Build successful! Preparing distribution...

:: 清理并创建发布目录
if exist dist_linux rmdir /s /q dist_linux
mkdir dist_linux
mkdir dist_linux\static

:: 复制文件
copy /y video-ai-helper-linux dist_linux\main
xcopy /s /y /i static dist_linux\static

echo.
echo Creating deployment instructions...

:: 生成部署说明文档
(
echo ==========================================
echo   Video AI Helper - Deployment Guide
echo ==========================================
echo.
echo 1. Upload:
echo    Upload the entire 'dist_linux' folder to your Linux server (e.g., /opt/video-ai).
echo.
echo 2. Permissions:
echo    cd /opt/video-ai
echo    chmod +x main
echo.
echo 3. Run Manually:
echo    ./main -mode server -port 8080
echo.
echo 4. Run as Service (Systemd):
echo    a. Create service file:
echo       sudo nano /etc/systemd/system/video-ai.service
echo.
echo    b. Paste content:
echo       [Unit]
echo       Description=Video AI Helper
echo       After=network.target
echo.
echo       [Service]
echo       User=root
echo       # IMPORTANT: WorkingDirectory must be where 'static' folder is
echo       WorkingDirectory=/opt/video-ai
echo       ExecStart=/opt/video-ai/main -mode server -port 8080
echo       Restart=always
echo.
echo       [Install]
echo       WantedBy=multi-user.target
echo.
echo    c. Start service:
echo       sudo systemctl daemon-reload
echo       sudo systemctl enable video-ai
echo       sudo systemctl start video-ai
echo.
echo 5. Verify:
echo    Check logs: sudo journalctl -u video-ai -f
echo    Visit: http://your-server-ip:8080
) > dist_linux\README.txt

echo.
echo ==========================================
echo   Done! Check the 'dist_linux' folder.
echo ==========================================
pause