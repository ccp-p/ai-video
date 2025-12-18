package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "time"

    "ccode/models"
)

// MockBcutASR 模拟的ASR服务，用于测试不依赖网络
type MockBcutASR struct {
    *BaseASR
}

// NewMockBcutASR 创建模拟ASR服务
func NewMockBcutASR(audioPath string, useCache bool) (*MockBcutASR, error) {
    baseASR, err := NewBaseASR(audioPath, useCache)
    if err != nil {
        return nil, err
    }
    return &MockBcutASR{BaseASR: baseASR}, nil
}

// GetResult 模拟获取识别结果
func (m *MockBcutASR) GetResult(ctx context.Context, callback ProgressCallback) ([]models.DataSegment, error) {
    instanceID := "MockTest-123456"
    log.Printf("[%s] 开始模拟ASR处理: %s", instanceID, m.AudioPath)

    // 检查缓存
    cacheKey := m.GetCacheKey("MockASR")
    if m.UseCache {
        if segments, ok := m.LoadFromCache("./cache", cacheKey); ok {
            log.Printf("[%s] 从缓存加载结果", instanceID)
            if callback != nil {
                callback(100, "识别完成 (缓存)")
            }
            return segments, nil
        }
    }

    // 模拟处理步骤（带进度和取消检查）
    steps := []struct {
        percent int
        message string
        sleep   time.Duration
    }{
        {20, "正在上传...", 500 * time.Millisecond},
        {50, "提交任务...", 500 * time.Millisecond},
        {60, "处理中 60%...", 800 * time.Millisecond},
        {75, "处理中 75%...", 800 * time.Millisecond},
        {90, "处理中 90%...", 600 * time.Millisecond},
    }

    for _, step := range steps {
        select {
        case <-ctx.Done():
            log.Printf("[%s] 任务取消", instanceID)
            return nil, ctx.Err()
        default:
        }

        if callback != nil {
            callback(step.percent, step.message)
        }
        time.Sleep(step.sleep)
    }

    // 模拟返回的识别结果
    segments := []models.DataSegment{
        {
            Text:      "欢迎使用Bcut ASR语音识别服务",
            StartTime: 0.5,
            EndTime:   3.2,
        },
        {
            Text:      "这是一段测试音频，用于验证代码功能",
            StartTime: 3.5,
            EndTime:   7.1,
        },
        {
            Text:      "识别结果将按照时间戳分段显示",
            StartTime: 7.5,
            EndTime:   10.8,
        },
    }

    // 保存到缓存
    if m.UseCache {
        _ = m.SaveToCache("./cache", cacheKey, segments)
    }

    if callback != nil {
        callback(100, "识别完成")
    }

    log.Printf("[%s] 模拟处理完成，共 %d 段结果", instanceID, len(segments))
    return segments, nil
}

func main() {
    fmt.Println("=== Bcut ASR 测试工具 (模拟模式) ===")
    fmt.Println("这个测试不依赖网络，用于验证代码逻辑\n")

    // 创建一个虚拟音频文件用于测试
    testAudioFile := "./test_audio.mp3"
    if _, err := os.Stat(testAudioFile); os.IsNotExist(err) {
        // 创建一个虚拟音频文件
        _ = os.WriteFile(testAudioFile, []byte("mock audio data for testing"), 0644)
        fmt.Printf("已创建测试文件: %s\n\n", testAudioFile)
    }

    // 创建服务（可选择是否使用缓存）
    useCache := true
    asrClient, err := NewMockBcutASR(testAudioFile, useCache)
    if err != nil {
        log.Fatalf("创建服务失败: %v", err)
    }

    // 创建带超时的context
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    // 进度回调
    progressCallback := func(percent int, message string) {
        fmt.Printf("\r进度: [%-50s] %d%% %s",
            string(make([]byte, percent/2)), percent, message)
    }

    fmt.Println("开始处理...")
    startTime := time.Now()

    // 获取结果
    segments, err := asrClient.GetResult(ctx, progressCallback)
    if err != nil {
        fmt.Printf("\n处理失败: %v\n", err)
        return
    }

    // 输出结果
    fmt.Printf("\n\n✅ 处理完成！耗时: %.2f秒\n", time.Since(startTime).Seconds())
    fmt.Printf("识别结果共 %d 段:\n\n", len(segments))

    for i, segment := range segments {
        fmt.Printf("[%d] 时间: %.2f - %.2f秒\n", i+1, segment.StartTime, segment.EndTime)
        fmt.Printf("    内容: %s\n\n", segment.Text)
    }

    fmt.Println("测试完成！检查上面的输出，如果看到结果，说明代码架构正常")
    fmt.Println("✅ 代码结构验证通过")
    fmt.Println("\n如果要连接真实的B站ASR服务，只需使用 BcutASR 类替换 MockBcutASR")
}
