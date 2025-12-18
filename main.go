package main

import (
    "context"
    "encoding/json"
    "flag"
    "fmt"
    "log"
    "os"
    "time"

    "ccode/models"
)

func main() {
    // 命令行参数
    audioFile := flag.String("audio", "", "音频文件路径")
    useCache := flag.Bool("cache", true, "是否使用缓存")
    timeout := flag.Int("timeout", 300, "超时时间(秒)")
    flag.Parse()

    if *audioFile == "" {
        fmt.Println("使用方法:")
        fmt.Println("  go run . -audio <音频文件路径> [-cache true/false] [-timeout 300]")
        fmt.Println("示例:")
        fmt.Println("  go run . -audio test.mp3")
        fmt.Println("  go run . -audio test.mp3 -cache false")
        return
    }

    fmt.Println("=== Bilibili Bcut ASR 语音识别工具 ===")
    fmt.Printf("音频文件: %s\n", *audioFile)
    fmt.Printf("缓存功能: %v\n", *useCache)
    fmt.Printf("超时时间: %d秒\n", *timeout)
    fmt.Println()

    // 创建Bcut ASR服务
    asrClient, err := NewBcutASR(*audioFile, *useCache)
    if err != nil {
        log.Fatalf("创建ASR服务失败: %v", err)
    }

    // 创建带超时的context
    ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeout)*time.Second)
    defer cancel()

    // 进度回调函数
    progressCallback := func(percent int, message string) {
        fmt.Printf("\r进度: [%-50s] %d%% %s",
            string(make([]byte, percent/2)), percent, message)
    }

    fmt.Println("开始处理...")

    // 开始识别
    startTime := time.Now()
    segments, err := asrClient.GetResult(ctx, progressCallback)
    if err != nil {
        fmt.Printf("\n处理失败: %v\n", err)
        return
    }

    // 输出结果
    fmt.Printf("\n\n✅ 识别完成！耗时: %.2f秒\n", time.Since(startTime).Seconds())
    fmt.Printf("识别结果共 %d 段:\n\n", len(segments))

    for i, segment := range segments {
        fmt.Printf("[%d] 时间: %.2f - %.2f秒\n", i+1, segment.StartTime, segment.EndTime)
        fmt.Printf("    内容: %s\n\n", segment.Text)
    }

    // 保存结果到文件
    outputFileName := "asr_result.json"
    if saveResultsToFile(segments, outputFileName) {
        fmt.Printf("结果已保存到: %s\n", outputFileName)
    }
}

// saveResultsToFile 保存识别结果到JSON文件
func saveResultsToFile(segments []models.DataSegment, filename string) bool {
    file, err := os.Create(filename)
    if err != nil {
        log.Printf("创建输出文件失败: %v", err)
        return false
    }
    defer file.Close()

    encoder := json.NewEncoder(file)
    encoder.SetIndent("", "  ")
    if err := encoder.Encode(segments); err != nil {
        log.Printf("写入输出文件失败: %v", err)
        return false
    }
    return true
}
