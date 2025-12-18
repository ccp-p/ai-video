package main

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "time"

    "ccode/models"
    "ccode/utils"
)

// Bcut ASR相关常量定义
const (
    API_BASE_URL     = "https://member.bilibili.com/x/bcut/rubick-interface"
    API_REQ_UPLOAD   = API_BASE_URL + "/resource/create"
    API_COMMIT_UPLOAD = API_BASE_URL + "/resource/create/complete"
    API_CREATE_TASK  = API_BASE_URL + "/task"
    API_QUERY_RESULT = API_BASE_URL + "/task/result"

    ModelIDUpload    = "8"
    ModelIDQuery     = "7"

    MaxRetries       = 500
    TimeOffset       = 0.105 // 时间偏移校正量(秒)
    TimeoutSeconds   = 30
    RetryBaseDelay   = time.Second
    RetryLongDelay   = time.Second * 3
)

// BcutASR 必剪语音识别实现
type BcutASR struct {
    *BaseASR
    taskID       string
    etags        []string
    inBossKey    string
    resourceID   string
    uploadID     string
    uploadURLs   []string
    perSize      int
    clips        int
    downloadURL  string
}

// NewBcutASR 创建必剪ASR实例
func NewBcutASR(audioPath string, useCache bool) (ASRService, error) {
    baseASR, err := NewBaseASR(audioPath, useCache)
    if err != nil {
        return nil, err
    }

    return &BcutASR{
        BaseASR: baseASR,
        etags:   make([]string, 0),
    }, nil
}

// GetResult 实现ASRService接口
func (b *BcutASR) GetResult(ctx context.Context, callback ProgressCallback) ([]models.DataSegment, error) {
    instanceID := fmt.Sprintf("BcutASR-%s", utils.GenerateRandomString(8))
    utils.Info("[%s] GetResult 开始处理音频: %s", instanceID, b.AudioPath)

    // 检查缓存
    cacheKey := b.GetCacheKey("BcutASR")
    if b.UseCache {
        if segments, ok := b.LoadFromCache("./cache", cacheKey); ok {
            utils.Info("[%s] 从缓存加载必剪ASR结果", instanceID)
            if callback != nil {
                callback(100, "识别完成 (缓存)")
            }
            utils.Info("[%s] GetResult 完成 (来自缓存)", instanceID)
            return segments, nil
        }
        utils.Info("[%s] 缓存未命中", instanceID)
    }

    // 上传阶段
    if callback != nil {
        callback(20, "正在上传...")
    }
    utils.Info("[%s] 开始上传...", instanceID)
    if err := b.upload(); err != nil {
        utils.Error("[%s] 上传失败: %v", instanceID, err)
        return nil, fmt.Errorf("必剪ASR上传失败: %w", err)
    }
    utils.Info("[%s] 上传完成", instanceID)

    // 创建任务阶段
    if callback != nil {
        callback(50, "提交任务...")
    }
    utils.Info("[%s] 开始创建任务...", instanceID)
    if err := b.createTask(); err != nil {
        utils.Error("[%s] 创建任务失败: %v", instanceID, err)
        return nil, fmt.Errorf("必剪ASR创建任务失败: %w", err)
    }
    utils.Info("[%s] 创建任务完成, TaskID: %s", instanceID, b.taskID)

    // 查询结果阶段
    if callback != nil {
        callback(60, "等待结果...")
    }
    utils.Info("[%s] 开始查询结果...", instanceID)
    result, err := b.queryResult(ctx, callback)
    if err != nil {
        utils.Error("[%s] 查询结果失败: %v", instanceID, err)
        return nil, fmt.Errorf("必剪ASR查询结果失败: %w", err)
    }
    utils.Info("[%s] 查询结果成功", instanceID)

    // 处理结果
    utils.Info("[%s] 开始处理结果...", instanceID)
    segments := b.makeSegments(result)
    utils.Info("[%s] 处理结果完成, 共 %d 段", instanceID, len(segments))

    if callback != nil {
        callback(100, "识别完成")
    }

    // 保存缓存
    if b.UseCache && len(segments) > 0 {
        utils.Info("[%s] 开始缓存结果...", instanceID)
        if err := b.SaveToCache("./cache", cacheKey, segments); err != nil {
            utils.Warn("[%s] 保存必剪ASR结果到缓存失败: %v", instanceID, err)
        } else {
            utils.Info("[%s] 缓存结果成功", instanceID)
        }
    }

    utils.Info("[%s] GetResult 完成", instanceID)
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

    // 使用安全的类型断言避免panic
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

    utils.Info("申请上传成功, 总计大小%dKB, %d分片, 分片大小%dKB: %s",
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

        utils.Info("开始上传分片%d: %d-%d", i, startRange, endRange)

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
            // 尝试从响应体获取
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
        utils.Info("分片%d上传成功: %s", i, etag)
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

    utils.Info("提交成功，获取下载URL: %s", b.downloadURL)
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

    utils.Info("任务已创建: %s", b.taskID)
    return nil
}

// queryResult 查询结果（带context取消和重试延迟优化）
func (b *BcutASR) queryResult(ctx context.Context, callback ProgressCallback) (map[string]interface{}, error) {
    client := &http.Client{Timeout: TimeoutSeconds * time.Second}
    instanceID := utils.GenerateRandomString(6)

    utils.Info("[BcutASR-%s] 开始轮询查询任务: %s", instanceID, b.taskID)

    for i := 0; i < MaxRetries; i++ {
        // 检查上下文取消
        select {
        case <-ctx.Done():
            utils.Info("[BcutASR-%s] 上下文取消，停止查询", instanceID)
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
            utils.Warn("[BcutASR-%s] 第 %d 次查询请求失败: %v，将重试", instanceID, i, err)
            time.Sleep(RetryBaseDelay)
            continue
        }

        body, err := io.ReadAll(resp.Body)
        resp.Body.Close()

        if err != nil {
            utils.Warn("[BcutASR-%s] 第 %d 次查询读取响应失败: %v，将重试", instanceID, i, err)
            time.Sleep(RetryBaseDelay)
            continue
        }

        var result map[string]interface{}
        if err := json.Unmarshal(body, &result); err != nil {
            utils.Warn("[BcutASR-%s] 第 %d 次查询JSON解析失败: %v，将重试", instanceID, i, err)
            time.Sleep(RetryBaseDelay)
            continue
        }

        data, ok := result["data"].(map[string]interface{})
        if !ok {
            utils.Warn("[BcutASR-%s] 第 %d 次查询响应格式错误，将重试", instanceID, i)
            time.Sleep(RetryBaseDelay)
            continue
        }

        state, ok := data["state"].(float64)
        if !ok {
            utils.Warn("[BcutASR-%s] 第 %d 次查询状态字段缺失，将重试", instanceID, i)
            time.Sleep(RetryBaseDelay)
            continue
        }

        utils.Debug("[BcutASR-%s] 第 %d 次查询，任务状态: %v", instanceID, i, state)

        if state == 4 { // 任务完成
            resultStr, ok := data["result"].(string)
            if !ok || resultStr == "" {
                utils.Warn("[BcutASR-%s] 任务完成但结果为空", instanceID)
                return nil, fmt.Errorf("任务完成但结果为空")
            }

            var resultData map[string]interface{}
            if err := json.Unmarshal([]byte(resultStr), &resultData); err != nil {
                return nil, fmt.Errorf("解析结果失败: %w", err)
            }
            utils.Info("[BcutASR-%s] 任务结果查询成功，第 %d 次查询", instanceID, i)
            return resultData, nil
        } else if state == 3 { // 任务失败
            utils.Error("[BcutASR-%s] 任务处理失败，状态码: %v", instanceID, state)
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

    utils.Error("[BcutASR-%s] 查询超时，%d次尝试后仍未完成", instanceID, MaxRetries)
    return nil, fmt.Errorf("任务超时未完成")
}

// makeSegments 处理识别结果
func (b *BcutASR) makeSegments(result map[string]interface{}) []models.DataSegment {
    segments := []models.DataSegment{}

    utterances, ok := result["utterances"].([]interface{})
    if !ok {
        utils.Warn("解析B站ASR结果失败: 未找到utterances数组")
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

        segments = append(segments, models.DataSegment{
            Text:      text,
            StartTime: startTime,
            EndTime:   endTime,
        })
    }

    return segments
}