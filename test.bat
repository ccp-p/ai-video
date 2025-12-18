@echo off
chcp 65001 >nul
echo ========================================
echo      项目结构验证脚本
echo ========================================
echo.

echo [1/4] 检查Go文件...
if exist "main.go" (
    echo   ✓ main.go 存在
) else (
    echo   ✗ main.go 缺失
    exit /b 1
)

echo.
echo [2/4] 检查前端文件...
if exist "static\index.html" (
    echo   ✓ static/index.html 存在
) else (
    echo   ✗ static/index.html 缺失
    exit /b 1
)

echo.
echo [3/4] 检查配置文件...
if exist "go.mod" (
    echo   ✓ go.mod 存在
) else (
    echo   ✗ go.mod 缺失
    exit /b 1
)

if exist "README.md" (
    echo   ✓ README.md 存在
) else (
    echo   ✗ README.md 缺失
    exit /b 1
)

echo.
echo [4/4] 检查启动脚本...
if exist "start_server.bat" (
    echo   ✓ start_server.bat 存在
) else (
    echo   ✗ start_server.bat 缺失
    exit /b 1
)

echo.
echo ========================================
echo  所有文件检查通过！
echo ========================================
echo.
echo 项目结构：
echo  ├── main.go              (后端服务)
echo  ├── static/
echo  │   └── index.html      (前端界面)
echo  ├── start_server.bat    (启动脚本)
echo  ├── README.md           (说明文档)
echo  └── go.mod              (依赖配置)
echo.
echo 使用方法：
echo  1. 检查FFmpeg是否安装: ffmpeg -version
echo  2. 启动服务: start_server.bat
echo  3. 访问: http://localhost:8080
echo.
pause
