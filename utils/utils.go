package utils

import (
    "crypto/rand"
    "encoding/hex"
    "fmt"
    "log"
    "time"
)

// Info 输出信息日志
func Info(format string, v ...interface{}) {
    log.Printf("[INFO] "+format, v...)
}

// Warn 输出警告日志
func Warn(format string, v ...interface{}) {
    log.Printf("[WARN] "+format, v...)
}

// Error 输出错误日志
func Error(format string, v ...interface{}) {
    log.Printf("[ERROR] "+format, v...)
}

// Debug 输出调试日志
func Debug(format string, v ...interface{}) {
    // 可以根据需要开启
    // log.Printf("[DEBUG] "+format, v...)
}

// GenerateRandomString 生成随机字符串
func GenerateRandomString(n int) string {
    bytes := make([]byte, n/2+1)
    rand.Read(bytes)
    return hex.EncodeToString(bytes)[:n]
}

// SleepWithProgress 带进度提示的等待
func SleepWithProgress(duration time.Duration, step time.Duration, message string) {
    elapsed := time.Duration(0)
    for elapsed < duration {
        time.Sleep(step)
        elapsed += step
        Info("%s %.1fs", message, elapsed.Seconds())
    }
}
