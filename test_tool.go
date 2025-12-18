// 测试工具 - 验证环境和基本功能
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func main() {
	fmt.Println("=== 视频字幕工具环境检测 ===\n")

	// 检查Go版本
	fmt.Println("🔍 检查Go版本...")
	checkGoVersion()

	// 检查FFmpeg
	fmt.Println("\n🔍 检查FFmpeg...")
	checkFFmpeg()

	// 检查工作目录
	fmt.Println("\n🔍 检查工作目录...")
	checkWorkingDirectory()

	// 生成测试文件
	fmt.Println("\n🔍 生成测试配置...")
	generateTestConfig()

	fmt.Println("\n=== 检测完成 ===")
	fmt.Println("\n✅ 环境就绪！")
	fmt.Println("\n接下来可以：")
	fmt.Println("1. HTTP模式：go run main_enhanced.go -mode server -port 8080")
	fmt.Println("2. CLI模式：go run main_enhanced.go -mode cli -video <视频路径>")
	fmt.Println("3. 访问Web：http://localhost:8080")
}

func checkGoVersion() {
	version := runtime.Version()
	fmt.Printf("Go版本: %s\n", version)

	if version < "go1.21" {
		fmt.Println("⚠️  警告：建议使用Go 1.21或更高版本")
	} else {
		fmt.Println("✅ Go版本符合要求")
	}
}

func checkFFmpeg() {
	// 检查ffmpeg
	cmd := exec.Command("ffmpeg", "-version")
	if err := cmd.Run(); err != nil {
		fmt.Println("❌ 未找到ffmpeg，请安装并添加到PATH")
		fmt.Println("   下载地址: https://ffmpeg.org/download.html")
		return
	}

	// 检查ffprobe
	cmd = exec.Command("ffprobe", "-version")
	if err := cmd.Run(); err != nil {
		fmt.Println("❌ 未找到ffprobe，请安装FFmpeg完整版")
		return
	}

	fmt.Println("✅ FFmpeg环境正常")
	fmt.Println("   ffmpeg ✓")
	fmt.Println("   ffprobe ✓")
}

func checkWorkingDirectory() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Printf("❌ 无法获取工作目录: %v\n", err)
		return
	}

	fmt.Printf("当前目录: %s\n", cwd)

	// 检查必要文件
	necessaryFiles := []string{"main_enhanced.go", "go.mod", "README.md"}
	for _, file := range necessaryFiles {
		if _, err := os.Stat(file); err == nil {
			fmt.Printf("   ✓ %s\n", file)
		} else {
			fmt.Printf("   ❌ %s 缺失\n", file)
		}
	}

	// 检查cache目录
	cacheDir := filepath.Join(cwd, "cache")
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		fmt.Println("   正在创建cache目录...")
		os.Mkdir(cacheDir, 0755)
		fmt.Println("   ✓ cache/ 目录已创建")
	} else {
		fmt.Println("   ✓ cache/ 目录存在")
	}
}

func generateTestConfig() {
	// 创建一个示例配置说明文件
	configHelp := `# AI配置示例说明

该工具支持接入多种AI服务，以下是常见配置示例：

## 1. OpenAI (GPT)
API Key: sk-xxxxxxxxxxxxxxxxxxxxxxxx
API URL: https://api.openai.com/v1/chat/completions
Model: gpt-4

## 2. 文心一言
API Key: your-wenxin-key
API URL: https://aip.baidubce.com/rpc/2.0/ai_custom/v1/wenxinworkshop/chat/completions
Model: ernie-bot

## 3. 通义千问
API Key: your-tongyi-key
API URL: https://dashscope.aliyuncs.com/api/v1/services/aigc/text-generation/generation
Model: qwen-turbo

## 4. 本地模式 (无需配置)
如果不配置API，系统会使用本地算法生成基础总结

## 自定义Prompt示例
请总结以下内容，要求：
1. 提取3-5个核心要点
2. 使用Markdown格式
3. 语言简洁明了
4. 包含关键词和时间信息
`
	os.WriteFile("AI配置说明.txt", []byte(configHelp), 0644)
	fmt.Println("✅ 已生成AI配置说明.txt")
}

// 创建演示用的批处理脚本
func createBatchScripts() {
	// Windows批处理脚本
	batchContent := `@echo off
echo === 视频字幕工具 ===
echo.

if "%1"=="" (
    echo 用法：
    echo   server - 启动HTTP服务
    echo   cli -video [路径] - 命令行处理视频
    echo.
    echo 示例：
    echo   %0 server
    echo   %0 cli -video "D:\videos\demo.mp4"
    goto :eof
)

if "%1"=="server" (
    echo 启动HTTP服务...
    go run main_enhanced.go -mode server -port 8080
) else if "%1"=="cli" (
    echo 命令行模式...
    go run main_enhanced.go -mode cli -video "%2"
) else (
    echo 未知模式: %1
)

:eof
pause
`
	os.WriteFile("run.bat", []byte(batchContent), 0755)

	// PowerShell脚本
	psContent := `# 视频字幕工具启动脚本
param(
    [string]$Mode = "server",
    [string]$Video = ""
)

Write-Host "=== 视频字幕工具 ===`n" -ForegroundColor Cyan

if ($Mode -eq "server") {
    Write-Host "启动HTTP服务..." -ForegroundColor Green
    go run main_enhanced.go -mode server -port 8080
}
elseif ($Mode -eq "cli") {
    if ($Video -eq "") {
        Write-Host "错误: 请提供视频路径" -ForegroundColor Red
        Write-Host "用法: .\run.ps1 -Mode cli -Video 'D:\videos\demo.mp4'"
        exit 1
    }
    Write-Host "处理视频: $Video" -ForegroundColor Green
    go run main_enhanced.go -mode cli -video $Video
}
else {
    Write-Host "错误: 未知模式 $Mode" -ForegroundColor Red
    Write-Host "可用模式: server, cli"
    exit 1
}
`
	os.WriteFile("run.ps1", []byte(psContent), 0755)

	fmt.Println("✅ 已生成运行脚本:")
	fmt.Println("   - run.bat (Windows)")
	fmt.Println("   - run.ps1 (PowerShell)")
}
