package main

import (
    "crypto/md5"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "io"
    "os"
    "path/filepath"

    "ccode/models"
    "ccode/utils"
)

// ProgressCallback 进度回调函数类型
type ProgressCallback func(percent int, message string)

// ASRService ASR服务接口
type ASRService interface {
    GetResult(ctx interface{}, callback ProgressCallback) ([]models.DataSegment, error)
}

// BaseASR ASR基类
type BaseASR struct {
    AudioPath string
    FileBinary []byte
    UseCache  bool
}

// NewBaseASR 创建基类实例
func NewBaseASR(audioPath string, useCache bool) (*BaseASR, error) {
    // 读取音频文件
    fileBytes, err := os.ReadFile(audioPath)
    if err != nil {
        return nil, fmt.Errorf("读取音频文件失败: %w", err)
    }

    return &BaseASR{
        AudioPath: audioPath,
        FileBinary: fileBytes,
        UseCache:  useCache,
    }, nil
}

// GetCacheKey 生成缓存键
func (b *BaseASR) GetCacheKey(serviceName string) string {
    // 使用文件路径和文件内容的MD5作为缓存键
    hash := md5.New()
    hash.Write([]byte(b.AudioPath))
    hash.Write(b.FileBinary)
    return fmt.Sprintf("%s_%s", serviceName, hex.EncodeToString(hash.Sum(nil)))
}

// LoadFromCache 从缓存加载结果
func (b *BaseASR) LoadFromCache(cacheDir string, cacheKey string) ([]models.DataSegment, bool) {
    cachePath := filepath.Join(cacheDir, cacheKey+".json")

    if _, err := os.Stat(cachePath); os.IsNotExist(err) {
        return nil, false
    }

    data, err := os.ReadFile(cachePath)
    if err != nil {
        utils.Warn("读取缓存失败: %v", err)
        return nil, false
    }

    var segments []models.DataSegment
    if err := json.Unmarshal(data, &segments); err != nil {
        utils.Warn("解析缓存失败: %v", err)
        return nil, false
    }

    return segments, true
}

// SaveToCache 保存结果到缓存
func (b *BaseASR) SaveToCache(cacheDir string, cacheKey string, segments []models.DataSegment) error {
    // 创建缓存目录
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
