// 全部整合在一个文件中 - Bilibili Bcut ASR 完整实现
// 运行前请设置: go mod init ccode && go mod tidy

package main

import (
    "bytes"
    "context"
    "crypto/md5"
    "crypto/rand"
    "encoding/hex"
    "encoding/json"
    "flag"
    "fmt"
    "io"
    "log"
    "net/http"
    "os"
    "path/filepath"
    "time"
)

// ==================== 常量定义 ====================
const (
    API_BASE_URL      = "https://member.bilibili.com/x/bcut/rubick-interface"
    API_REQ_UPLOAD    = API_BASE_URL + "/resource/create"
    API_COMMIT_UPLOAD = API_BASE_URL + "/resource/create/complete"
    API_CREATE_TASK   = API_BASE_URL + "/task"
    API_QUERY_RESULT  = API_BASE_URL + "/task/result"

    ModelIDUpload = "8"
    ModelIDQuery  = "7"

    MaxRetries     = 500
    TimeOffset     = 0.105 // 时间偏移校正量(秒)
    TimeoutSeconds = 30
    RetryBaseDelay = time.Second
    RetryLongDelay = time.Second * 3
)

// ==================== 数据结构 ====================

// DataSegment 识别结果数据段
type DataSegment struct {
    Text      string  `json:"text"`
    StartTime float64 `json:"start_time"`
    EndTime   float64 `json:"end_time"`
}

// ProgressCallback 进度回调函数类型
type ProgressCallback func(percent int, message string)

// ==================== 工具函数 ====================

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
    // log.Printf("[DEBUG] "+format, v...)
}

// GenerateRandomString 生成随机字符串
func GenerateRandomString(n int) string {
    bytes := make([]byte, n/2+1)
    rand.Read(bytes)
    return hex.EncodeToString(bytes)[:n]
}

// ==================== ASR 基类 ====================

// BaseASR ASR基类
type BaseASR struct {
    AudioPath  string
    FileBinary []byte
    UseCache   bool
}

// NewBaseASR 创建基类实例
func NewBaseASR(audioPath string, useCache bool) (*BaseASR, error) {
    fileBytes, err := os.ReadFile(audioPath)
    if err != nil {
        return nil, fmt.Errorf("读取音频文件失败: %w", err)
    }

    return &BaseASR{
        AudioPath:  audioPath,
        FileBinary: fileBytes,
        UseCache:   useCache,
    }, nil
}

// GetCacheKey 生成缓存键
func (b *BaseASR) GetCacheKey(serviceName string) string {
    hash := md5.New()
    hash.Write([]byte(b.AudioPath))
    hash.Write(b.FileBinary)
    return fmt.Sprintf("%s_%s", serviceName, hex.EncodeToString(hash.Sum(nil)))
}

// LoadFromCache 从缓存加载
func (b *BaseASR) LoadFromCache(cacheDir string, cacheKey string) ([]DataSegment, bool) {
    cachePath := filepath.Join(cacheDir, cacheKey+".json")
    if _, err := os.Stat(cachePath); os.IsNotExist(err) {
        return nil, false
    }

    data, err := os.ReadFile(cachePath)
    if err != nil {
        Warn("读取缓存失败: %v", err)
        return nil, false
    }

    var segments []DataSegment
    if err := json.Unmarshal(data, &segments); err != nil {
        Warn("解析缓存失败: %v", err)
        return nil, false
    }

    return segments, true
}

// SaveToCache 保存缓存
func (b *BaseASR) SaveToCache(cacheDir string, cacheKey string, segments []DataSegment) error {
    if err := os.MkdirAll(cacheDir, 0755); err != nil {
        return fmt.Errorf("创建缓存目录失败: %w", err)
    }

    cachePath := filepath.Join(cacheDir, cacheKey+".json")
    data, err := json.MarshalIndent(segments, "", "  ")
    if err != nil {
        return fmt.Errorf("序列化缓存失败: %w", err)
    }

    if err := os.WriteFile(cachePath, data, 0644); err != nil {
        return fmt.Errorf("写入缓存失败: %w", err)
    }

    return nil
}

// ==================== Bcut ASR 实现 ====================

// BcutASR 必剪语音识别实现
type BcutASR struct {
    *BaseASR
    taskID      string
    etags       []string
    inBossKey   string
    resourceID  string
    uploadID    string
    uploadURLs  []string
    perSize     int
    clips       int
    downloadURL string
}

// NewBcutASR 创建必剪ASR实例
func NewBcutASR(audioPath string, useCache bool) (*BcutASR, error) {
    baseASR, err := NewBaseASR(audioPath, useCache)
    if err != nil {
        return nil, err
    }

    return &BcutASR{
        BaseASR: baseASR,
        etags:   make([]string, 0),
    }, nil
}

// GetResult 实现ASR服务接口
func (b *BcutASR) GetResult(ctx context.Context, callback ProgressCallback) ([]DataSegment, error) {
    instanceID := fmt.Sprintf("BcutASR-%s", GenerateRandomString(8))
    Info("[%s] GetResult 开始处理音频: %s", instanceID, b.AudioPath)

    // 检查缓存
    cacheKey := b.GetCacheKey("BcutASR")
    if b.UseCache {
        if segments, ok := b.LoadFromCache("./cache", cacheKey); ok {
            Info("[%s] 从缓存加载必剪ASR结果", instanceID)
            if callback != nil {
                callback(100, "识别完成 (缓存)")
            }
            Info("[%s] GetResult 完成 (来自缓存)", instanceID)
            return segments, nil
        }
        Info("[%s] 缓存未命中", instanceID)
    }

    // 上传阶段
    if callback != nil {
        callback(20, "正在上传...")
    }
    Info("[%s] 开始上传...", instanceID)
    if err := b.upload(); err != nil {
        Error("[%s] 上传失败: %v", instanceID, err)
        return nil, fmt.Errorf("必剪ASR上传失败: %w", err)
    }
    Info("[%s] 上传完成", instanceID)

    // 创建任务阶段
    if callback != nil {
        callback(50, "提交任务...")
    }
    Info("[%s] 开始创建任务...", instanceID)
    if err := b.createTask(); err != nil {
        Error("[%s] 创建任务失败: %v", instanceID, err)
        return nil, fmt.Errorf("必剪ASR创建任务失败: %w", err)
    }
    Info("[%s] 创建任务完成, TaskID: %s", instanceID, b.taskID)

    // 查询结果阶段
    if callback != nil {
        callback(60, "等待结果...")
    }
    Info("[%s] 开始查询结果...", instanceID)
    result, err := b.queryResult(ctx, callback)
    if err != nil {
        Error("[%s] 查询结果失败: %v", instanceID, err)
        return nil, fmt.Errorf("必剪ASR查询结果失败: %w", err)
    }
    Info("[%s] 查询结果成功", instanceID)

    // 处理结果
    Info("[%s] 开始处理结果...", instanceID)
    segments := b.makeSegments(result)
    Info("[%s] 处理结果完成, 共 %d 段", instanceID, len(segments))

    if callback != nil {
        callback(100, "识别完成")
    }

    // 保存缓存
    if b.UseCache && len(segments) > 0 {
        Info("[%s] 开始缓存结果...", instanceID)
        if err := b.SaveToCache("./cache", cacheKey, segments); err != nil {
            Warn("[%s] 保存必剪ASR结果到缓存失败: %v", instanceID, err)
        } else {
            Info("[%s] 缓存结果成功", instanceID)
        }
    }

    Info("[%s] GetResult 完成", instanceID)
    return segments, nil
}

// upload 上传文件主流程
func (b *BcutASR) upload() error {
    if err := b.requestUpload(); err != nil {
        return err
    }
    if err := b.uploadParts(); err != nil {
        return err
    }
    if err := b.commitUpload(); err != nil {
        return err
    }
    return nil
}

// requestUpload 申请上传（带安全的类型断言和超时）
func (b *BcutASR) requestUpload() error {
    payload := map[string]interface{}{
        "type":             2,
        "name":             "audio.mp3",
        "size":             len(b.FileBinary),
        "ResourceFileType": "mp3",
        "model_id":         ModelIDUpload,
    }

    jsonPayload, err := json.Marshal(payload)
    if err != nil {
        return fmt.Errorf("JSON编码失败: %w", err)
    }

    req, err := http.NewRequest("POST", API_REQ_UPLOAD, bytes.NewBuffer(jsonPayload))
    if err != nil {
        return fmt.Errorf("创建HTTP请求失败: %w", err)
    }

    req.Header.Set("User-Agent", "Bilibili/1.0.0 (https://www.bilibili.com)")
    req.Header.Set("Content-Type", "application/json")

    client := &http.Client{Timeout: TimeoutSeconds * time.Second}
    resp, err := client.Do(req)
    if err != nil {
        return fmt.Errorf("发送HTTP请求失败: %w", err)
    }
    defer resp.Body.Close()

    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return fmt.Errorf("读取响应失败: %w", err)
    }

    var result map[string]interface{}
    if err := json.Unmarshal(body, &result); err != nil {
        return fmt.Errorf("解析JSON响应失败: %w", err)
    }

    // 安全的类型断言
    data, ok := result["data"].(map[string]interface{})
    if !ok {
        return fmt.Errorf("响应格式错误: 缺少data字段")
    }

    if inBossKey, ok := data["in_boss_key"].(string); ok {
        b.inBossKey = inBossKey
    } else {
        return fmt.Errorf("in_boss_key字段缺失或类型错误")
    }

    if resourceID, ok := data["resource_id"].(string); ok {
        b.resourceID = resourceID
    } else {
        return fmt.Errorf("resource_id字段缺失或类型错误")
    }

    if uploadID, ok := data["upload_id"].(string); ok {
        b.uploadID = uploadID
    } else {
        return fmt.Errorf("upload_id字段缺失或类型错误")
    }

    if perSize, ok := data["per_size"].(float64); ok {
        b.perSize = int(perSize)
    } else {
        return fmt.Errorf("per_size字段缺失或类型错误")
    }

    uploadURLsIface, ok := data["upload_urls"].([]interface{})
    if !ok {
        return fmt.Errorf("upload_urls字段缺失或类型错误")
    }

    b.uploadURLs = make([]string, len(uploadURLsIface))
    for i, url := range uploadURLsIface {
        if urlStr, ok := url.(string); ok {
            b.uploadURLs[i] = urlStr
        } else {
            return fmt.Errorf("upload_urls[%d]类型错误", i)
        }
    }

    b.clips = len(b.uploadURLs)

    Info("申请上传成功, 总计大小%dKB, %d分片, 分片大小%dKB: %s",
        len(b.FileBinary)/1024, b.clips, b.perSize/1024, b.inBossKey)

    return nil
}

// uploadParts 上传分片
func (b *BcutASR) uploadParts() error {
    b.etags = make([]string, b.clips)
    client := &http.Client{Timeout: TimeoutSeconds * time.Second}

    for i := 0; i < b.clips; i++ {
        startRange := i * b.perSize
        endRange := (i + 1) * b.perSize
        if endRange > len(b.FileBinary) {
            endRange = len(b.FileBinary)
        }

        Info("开始上传分片%d: %d-%d", i, startRange, endRange)

        req, err := http.NewRequest("PUT", b.uploadURLs[i], bytes.NewBuffer(b.FileBinary[startRange:endRange]))
        if err != nil {
            return fmt.Errorf("创建HTTP请求失败: %w", err)
        }

        req.Header.Set("User-Agent", "Bilibili/1.0.0 (https://www.bilibili.com)")
        req.Header.Set("Content-Type", "application/octet-stream")

        resp, err := client.Do(req)
        if err != nil {
            return fmt.Errorf("发送HTTP请求失败: %w", err)
        }

        etag := resp.Header.Get("Etag")
        if etag == "" {
            body, _ := io.ReadAll(resp.Body)
            var result map[string]interface{}
            if json.Unmarshal(body, &result) == nil {
                if etagVal, ok := result["etag"].(string); ok {
                    etag = etagVal
                }
            }
        }
        resp.Body.Close()

        if etag == "" {
            return fmt.Errorf("分片%d上传失败: 未获取到Etag", i)
        }

        b.etags[i] = etag
        Info("分片%d上传成功: %s", i, etag)
    }

    return nil
}

// commitUpload 提交上传
func (b *BcutASR) commitUpload() error {
    payload := map[string]interface{}{
        "InBossKey":  b.inBossKey,
        "ResourceId": b.resourceID,
        "Etags":      b.buildEtags(),
        "UploadId":   b.uploadID,
        "model_id":   ModelIDUpload,
    }

    jsonPayload, err := json.Marshal(payload)
    if err != nil {
        return fmt.Errorf("JSON编码失败: %w", err)
    }

    req, err := http.NewRequest("POST", API_COMMIT_UPLOAD, bytes.NewBuffer(jsonPayload))
    if err != nil {
        return fmt.Errorf("创建HTTP请求失败: %w", err)
    }

    req.Header.Set("User-Agent", "Bilibili/1.0.0 (https://www.bilibili.com)")
    req.Header.Set("Content-Type", "application/json")

    client := &http.Client{Timeout: TimeoutSeconds * time.Second}
    resp, err := client.Do(req)
    if err != nil {
        return fmt.Errorf("发送HTTP请求失败: %w", err)
    }
    defer resp.Body.Close()

    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return fmt.Errorf("读取响应失败: %w", err)
    }

    var result map[string]interface{}
    if err := json.Unmarshal(body, &result); err != nil {
        return fmt.Errorf("解析JSON响应失败: %w", err)
    }

    data, ok := result["data"].(map[string]interface{})
    if !ok {
        return fmt.Errorf("响应格式错误")
    }

    if downloadURL, ok := data["download_url"].(string); ok {
        b.downloadURL = downloadURL
    } else {
        return fmt.Errorf("download_url字段缺失或类型错误")
    }

    Info("提交成功，获取下载URL: %s", b.downloadURL)
    return nil
}

// buildEtags 构建Etags字符串
func (b *BcutASR) buildEtags() string {
    etagsStr := ""
    for i, etag := range b.etags {
        if i > 0 {
            etagsStr += ","
        }
        etagsStr += etag
    }
    return etagsStr
}

// createTask 创建任务
func (b *BcutASR) createTask() error {
    payload := map[string]interface{}{
        "resource": b.downloadURL,
        "model_id": ModelIDUpload,
    }

    jsonPayload, err := json.Marshal(payload)
    if err != nil {
        return fmt.Errorf("JSON编码失败: %w", err)
    }

    req, err := http.NewRequest("POST", API_CREATE_TASK, bytes.NewBuffer(jsonPayload))
    if err != nil {
        return fmt.Errorf("创建HTTP请求失败: %w", err)
    }

    req.Header.Set("User-Agent", "Bilibili/1.0.0 (https://www.bilibili.com)")
    req.Header.Set("Content-Type", "application/json")

    client := &http.Client{Timeout: TimeoutSeconds * time.Second}
    resp, err := client.Do(req)
    if err != nil {
        return fmt.Errorf("发送HTTP请求失败: %w", err)
    }
    defer resp.Body.Close()

    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return fmt.Errorf("读取响应失败: %w", err)
    }

    var result map[string]interface{}
    if err := json.Unmarshal(body, &result); err != nil {
        return fmt.Errorf("解析JSON响应失败: %w", err)
    }

    data, ok := result["data"].(map[string]interface{})
    if !ok {
        return fmt.Errorf("响应格式错误")
    }

    if taskID, ok := data["task_id"].(string); ok {
        b.taskID = taskID
    } else {
        return fmt.Errorf("task_id字段缺失或类型错误")
    }

    Info("任务已创建: %s", b.taskID)
    return nil
}

// queryResult 查询结果
func (b *BcutASR) queryResult(ctx context.Context, callback ProgressCallback) (map[string]interface{}, error) {
    client := &http.Client{Timeout: TimeoutSeconds * time.Second}
    instanceID := GenerateRandomString(6)

    Info("[BcutASR-%s] 开始轮询查询任务: %s", instanceID, b.taskID)

    for i := 0; i < MaxRetries; i++ {
        select {
        case <-ctx.Done():
            Info("[BcutASR-%s] 上下文取消，停止查询", instanceID)
            return nil, ctx.Err()
        default:
        }

        url := fmt.Sprintf("%s?model_id=%s&task_id=%s", API_QUERY_RESULT, ModelIDQuery, b.taskID)
        req, err := http.NewRequest("GET", url, nil)
        if err != nil {
            return nil, fmt.Errorf("创建HTTP请求失败: %w", err)
        }

        req.Header.Set("User-Agent", "Bilibili/1.0.0 (https://www.bilibili.com)")
        req.Header.Set("Content-Type", "application/json")

        resp, err := client.Do(req)
        if err != nil {
            Warn("[BcutASR-%s] 第 %d 次查询请求失败: %v，将重试", instanceID, i, err)
            time.Sleep(RetryBaseDelay)
            continue
        }

        body, err := io.ReadAll(resp.Body)
        resp.Body.Close()

        if err != nil {
            Warn("[BcutASR-%s] 第 %d 次查询读取响应失败: %v，将重试", instanceID, i, err)
            time.Sleep(RetryBaseDelay)
            continue
        }

        var result map[string]interface{}
        if err := json.Unmarshal(body, &result); err != nil {
            Warn("[BcutASR-%s] 第 %d 次查询JSON解析失败: %v，将重试", instanceID, i, err)
            time.Sleep(RetryBaseDelay)
            continue
        }

        data, ok := result["data"].(map[string]interface{})
        if !ok {
            Warn("[BcutASR-%s] 第 %d 次查询响应格式错误，将重试", instanceID, i)
            time.Sleep(RetryBaseDelay)
            continue
        }

        state, ok := data["state"].(float64)
        if !ok {
            Warn("[BcutASR-%s] 第 %d 次查询状态字段缺失，将重试", instanceID, i)
            time.Sleep(RetryBaseDelay)
            continue
        }

        Debug("[BcutASR-%s] 第 %d 次查询，任务状态: %v", instanceID, i, state)

        if state == 4 { // 任务完成
            resultStr, ok := data["result"].(string)
            if !ok || resultStr == "" {
                Warn("[BcutASR-%s] 任务完成但结果为空", instanceID)
                return nil, fmt.Errorf("任务完成但结果为空")
            }

            var resultData map[string]interface{}
            if err := json.Unmarshal([]byte(resultStr), &resultData); err != nil {
                return nil, fmt.Errorf("解析结果失败: %w", err)
            }
            Info("[BcutASR-%s] 任务结果查询成功，第 %d 次查询", instanceID, i)
            return resultData, nil
        } else if state == 3 { // 任务失败
            Error("[BcutASR-%s] 任务处理失败，状态码: %v", instanceID, state)
            return nil, fmt.Errorf("任务处理失败，状态: %v", state)
        }

        // 更新进度
        if callback != nil && i%5 == 0 {
            progress := 60 + int(float64(i)/float64(MaxRetries)*39)
            if progress > 99 {
                progress = 99
            }
            callback(progress, fmt.Sprintf("处理中 %d%%...", progress))
        }

        // 动态等待时间
        sleepDuration := RetryBaseDelay
        if i > 20 {
            sleepDuration = RetryBaseDelay * 2
        }
        if i > 50 {
            sleepDuration = RetryLongDelay
        }
        time.Sleep(sleepDuration)
    }

    Error("[BcutASR-%s] 查询超时，%d次尝试后仍未完成", instanceID, MaxRetries)
    return nil, fmt.Errorf("任务超时未完成")
}

// makeSegments 处理识别结果
func (b *BcutASR) makeSegments(result map[string]interface{}) []DataSegment {
    segments := []DataSegment{}

    utterances, ok := result["utterances"].([]interface{})
    if !ok {
        Warn("解析B站ASR结果失败: 未找到utterances数组")
        return segments
    }

    for _, u := range utterances {
        utterance, ok := u.(map[string]interface{})
        if !ok {
            continue
        }

        text, _ := utterance["transcript"].(string)
        startTimeRaw, _ := utterance["start_time"].(float64)
        endTimeRaw, _ := utterance["end_time"].(float64)

        // 时间转换：API时间值/1000 + 偏移量
        startTime := startTimeRaw/1000.0 + TimeOffset
        endTime := endTimeRaw/1000.0 + TimeOffset

        segments = append(segments, DataSegment{
            Text:      text,
            StartTime: startTime,
            EndTime:   endTime,
        })
    }

    return segments
}

// ==================== 主程序 ====================

func main() {
    // 命令行参数
    audioFile := flag.String("audio", "", "音频文件路径")
    useCache := flag.Bool("cache", true, "是否使用缓存")
    timeout := flag.Int("timeout", 300, "超时时间(秒)")
    flag.Parse()

    if *audioFile == "" {
        fmt.Println("=== Bilibili Bcut ASR 语音识别工具 ===")
        fmt.Println("\n使用方法:")
        fmt.Println("  go run ccode.go -audio <音频文件路径> [-cache true/false] [-timeout 300]")
        fmt.Println("\n示例:")
        fmt.Println("  go run ccode.go -audio test.mp3")
        fmt.Println("  go run ccode.go -audio test.mp3 -cache false")
        fmt.Println("\n注意:")
        fmt.Println("  - 需要B站账号cookie，可能需要登录验证")
        fmt.Println("  - 文件会先上传到B站服务器，然后调用ASR服务")
        fmt.Println("  - 结果会自动保存")
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
    outputFileName := fmt.Sprintf("asr_result_%d.json", time.Now().Unix())
    if saveResultsToFile(segments, outputFileName) {
        fmt.Printf("结果已保存到: %s\n", outputFileName)
    }
}

// saveResultsToFile 保存识别结果到JSON文件
func saveResultsToFile(segments []DataSegment, filename string) bool {
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
