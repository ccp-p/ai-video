// 视频转字幕 - 后端服务
// 功能：视频音频提取 + 语音识别 + SRT字幕生成 + HTTP服务 + AI总结
// 前后端分离版本
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
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ==================== 常量定义 ====================
const (
	// ASR API
	API_BASE_URL      = "https://member.bilibili.com/x/bcut/rubick-interface"
	API_REQ_UPLOAD    = API_BASE_URL + "/resource/create"
	API_COMMIT_UPLOAD = API_BASE_URL + "/resource/create/complete"
	API_CREATE_TASK   = API_BASE_URL + "/task"
	API_QUERY_RESULT  = API_BASE_URL + "/task/result"

	ModelIDUpload = "8"
	ModelIDQuery  = "7"

	MaxRetries     = 500
	TimeOffset     = 0.105
	TimeoutSeconds = 30
	RetryBaseDelay = time.Second
	RetryLongDelay = time.Second * 3

	// HTTP 服务
	HTTP_PORT    = "8080"
	DOWNLOAD_DIR = "D:/download"
)

// ==================== 数据结构 ====================

// DataSegment 识别结果数据段
type DataSegment struct {
	Text      string  `json:"text"`
	StartTime float64 `json:"start_time"`
	EndTime   float64 `json:"end_time"`
}

// SRTItem SRT字幕项
type SRTItem struct {
	Index     int
	StartTime string
	EndTime   string
	Text      string
}

// AIConfig AI配置
type AIConfig struct {
	APIKey      string `json:"api_key"`
	APIURL      string `json:"api_url"`
	Model       string `json:"model"`
	CustomPrompt string `json:"custom_prompt"`
}

// AIRequest AI请求
type AIRequest struct {
	Text        string        `json:"text"`
	Prompt      string        `json:"prompt"`
	Segments    []DataSegment `json:"segments"`
	Screenshots []string      `json:"screenshots"`
	VideoPath   string        `json:"video_path"` // 必须传入视频路径以进行截图
}

// ChatRequest AI对话请求
type ChatRequest struct {
	History []map[string]string `json:"history"` // 历史对话
	Context string              `json:"context"` // 当前文档内容
	Message string              `json:"message"` // 用户提问
}

// AIResponse AI响应
type AIResponse struct {
	Summary  string   `json:"summary"`
	Markdown string   `json:"markdown"`
	Points   []string `json:"points"`
	Success  bool     `json:"success"`
}

// FileItem 文件列表项
type FileItem struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	ModTime  string `json:"mod_time"`
	Type     string `json:"type"` // video, audio, other
}

// ProcessRequest 处理请求
type ProcessRequest struct {
	VideoPath string `json:"video_path"`
	CheckOnly bool   `json:"check_only"` // 新增：仅检查状态
}

// ProcessResponse 处理响应
type ProcessResponse struct {
	Success       bool          `json:"success"`
	Message       string        `json:"message,omitempty"`
	AudioPath     string        `json:"audio_path,omitempty"`
	SrtPath       string        `json:"srt_path,omitempty"`
	SrtContent    string        `json:"srt_content,omitempty"`
	Segments      []DataSegment `json:"segments,omitempty"`
	Screenshots   []string      `json:"screenshots,omitempty"`
	OutputDir     string        `json:"output_dir,omitempty"`
	Duration      float64       `json:"duration,omitempty"`
	SegmentCount  int           `json:"segment_count,omitempty"`
	AIResult      *AIResponse   `json:"ai_result,omitempty"` // 新增：返回缓存的AI总结
}

// ProgressCallback 进度回调函数类型
type ProgressCallback func(percent int, message string)

// ==================== 工具函数 ====================

// getHTTPClient 获取带代理的HTTP客户端
func getHTTPClient() *http.Client {
	// proxyURL, err := url.Parse("http://127.0.0.1:7890")
	// if err != nil {
	// 	return &http.Client{Timeout: TimeoutSeconds * time.Second}
	// }
		// Proxy: http.ProxyURL(proxyURL),

	return &http.Client{
		Transport: &http.Transport{
		},
		Timeout: TimeoutSeconds * time.Second,
	}
}

func Info(format string, v ...interface{}) {
	log.Printf("[INFO] "+format, v...)
}

func Warn(format string, v ...interface{}) {
	log.Printf("[WARN] "+format, v...)
}

func Error(format string, v ...interface{}) {
	log.Printf("[ERROR] "+format, v...)
}

func GenerateRandomString(n int) string {
	bytes := make([]byte, n/2+1)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)[:n]
}

// ==================== 视频处理工具 ====================

// VideoProcessor 视频处理器
type VideoProcessor struct {
	VideoPath string
	OutputDir string
}

// NewVideoProcessor 创建视频处理器
func NewVideoProcessor(videoPath string) (*VideoProcessor, error) {
	// 检查ffmpeg是否存在
	_, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("未找到ffmpeg，请确保已安装并添加到PATH: %v", err)
	}

	// 获取视频文件绝对路径
	absPath, err := filepath.Abs(videoPath)
	if err != nil {
		return nil, fmt.Errorf("获取文件路径失败: %v", err)
	}

	// 创建输出目录
	outputDir := filepath.Join(filepath.Dir(absPath), "output_"+filepath.Base(absPath))
	os.MkdirAll(outputDir, 0755)

	return &VideoProcessor{
		VideoPath: absPath,
		OutputDir: outputDir,
	}, nil
}

// ArchiveAndClean 归档并清理 (替代原 DeleteOutput)
// 1. 删除原视频
// 2. 清理中间文件(audio, srt, segments)
// 3. 将 summary.json 和 截图 移动到 archive 目录
func (vp *VideoProcessor) ArchiveAndClean() error {
	// 1. 删除原视频
	if err := os.Remove(vp.VideoPath); err != nil && !os.IsNotExist(err) {
		Warn("删除视频失败: %v", err)
	} else {
		Info("已删除原视频: %s", vp.VideoPath)
	}

	if vp.OutputDir == "" || !strings.Contains(vp.OutputDir, "output_") {
		return nil
	}

	// 2. 准备归档目录
	// 假设 OutputDir 是 D:/download/output_xxx
	// archiveDir 是 D:/download/archive
	baseDir := filepath.Dir(vp.OutputDir)
	archiveRoot := filepath.Join(baseDir, "archive")
	if err := os.MkdirAll(archiveRoot, 0755); err != nil {
		return fmt.Errorf("创建归档目录失败: %v", err)
	}

	// 3. 清理 OutputDir 中的无关文件，保留 summary 和 图片
	entries, err := os.ReadDir(vp.OutputDir)
	if err != nil {
		return err
	}

	hasContent := false
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(vp.OutputDir, name)
		ext := strings.ToLower(filepath.Ext(name))

		// 保留 summary.json 和 图片
		if name == "summary.json" || ext == ".jpg" || ext == ".png" || ext == ".jpeg" {
			hasContent = true
			continue
		}
		// 删除其他文件 (audio.mp3, segments.json, subtitles.srt 等)
		os.Remove(path)
	}

	// 4. 移动文件夹到 archive
	if hasContent {
		destPath := filepath.Join(archiveRoot, filepath.Base(vp.OutputDir))
		os.RemoveAll(destPath) // 如果已存在则覆盖
		if err := os.Rename(vp.OutputDir, destPath); err != nil {
			Warn("归档移动失败: %v", err)
		} else {
			Info("已归档总结和截图到: %s", destPath)
		}
	} else {
		// 如果没有重要内容，直接删除文件夹
		os.RemoveAll(vp.OutputDir)
		Info("输出目录无重要内容，已删除")
	}

	return nil
}

// ExtractAudio 从视频提取音频
func (vp *VideoProcessor) ExtractAudio() (string, error) {
	audioPath := filepath.Join(vp.OutputDir, "audio.mp3")

	// 检查音频文件是否已存在，如果存在则直接复用
	if _, err := os.Stat(audioPath); err == nil {
		Info("检测到已存在的音频文件，跳过提取: %s", audioPath)
		return audioPath, nil
	}

	cmd := exec.Command("ffmpeg", "-i", vp.VideoPath, "-vn", "-acodec", "libmp3lame",
		"-ac", "2", "-ar", "16000", "-y", audioPath)

	_, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("提取音频失败: %v", err)
	}

	Info("音频提取成功: %s", audioPath)
	return audioPath, nil
}

// ExtractScreenshots 提取视频截图 (保留此方法工具，但在流程中改为按需提取)
func (vp *VideoProcessor) ExtractScreenshots(duration float64) ([]string, error) {
	screenshotCount := 5 // 提取5张截图
	screenshotInterval := duration / float64(screenshotCount+1)

	screenshots := []string{}

	for i := 1; i <= screenshotCount; i++ {
		timeOffset := float64(i) * screenshotInterval
		screenshotPath := filepath.Join(vp.OutputDir, fmt.Sprintf("screenshot_%d.jpg", i))

		cmd := exec.Command("ffmpeg", "-ss", fmt.Sprintf("%.2f", timeOffset),
			"-i", vp.VideoPath, "-vframes", "1", "-q:v", "2", "-y", screenshotPath)

		_, err := cmd.CombinedOutput()
		if err != nil {
			Warn("截图 %d 失败: %v", i, err)
			continue
		}

		screenshots = append(screenshots, screenshotPath)
		Info("创建截图: %s", screenshotPath)
	}

	return screenshots, nil
}

// ExtractScreenshotAt 在指定时间点提取截图
func (vp *VideoProcessor) ExtractScreenshotAt(seconds float64) (string, error) {
	filename := fmt.Sprintf("ai_capture_%.2f.jpg", seconds)
	screenshotPath := filepath.Join(vp.OutputDir, filename)

	// 如果文件已存在，直接返回
	if _, err := os.Stat(screenshotPath); err == nil {
		return screenshotPath, nil
	}

	cmd := exec.Command("ffmpeg", "-ss", fmt.Sprintf("%.2f", seconds),
		"-i", vp.VideoPath, "-vframes", "1", "-q:v", "2", "-y", screenshotPath)

	if err := cmd.Run(); err != nil {
		return "", err
	}
	return screenshotPath, nil
}

// GetVideoDuration 获取视频时长
func (vp *VideoProcessor) GetVideoDuration() (float64, error) {
	cmd := exec.Command("ffprobe", "-v", "quiet", "-show_entries",
		"format=duration", "-of", "csv=p=0", vp.VideoPath)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("获取视频时长失败: %v", err)
	}

	durationStr := strings.TrimSpace(string(output))
	duration, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return 0, fmt.Errorf("解析时长失败: %v", err)
	}

	return duration, nil
}

// ==================== ASR相关 ====================

// BaseASR ASR基类
type BaseASR struct {
	AudioPath  string
	FileBinary []byte
	UseCache   bool
}

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

func (b *BaseASR) GetCacheKey(serviceName string) string {
	hash := md5.New()
	hash.Write([]byte(b.AudioPath))
	hash.Write(b.FileBinary)
	return fmt.Sprintf("%s_%s", serviceName, hex.EncodeToString(hash.Sum(nil)))
}

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

// BcutASR 必剪语音识别
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
			return segments, nil
		}
		Info("[%s] 缓存未命中", instanceID)
	}

	// 上传阶段
	if callback != nil {
		callback(20, "正在上传...")
	}
	if err := b.upload(); err != nil {
		Error("[%s] 上传失败: %v", instanceID, err)
		return nil, fmt.Errorf("必剪ASR上传失败: %w", err)
	}

	// 创建任务阶段
	if callback != nil {
		callback(50, "提交任务...")
	}
	if err := b.createTask(); err != nil {
		Error("[%s] 创建任务失败: %v", instanceID, err)
		return nil, fmt.Errorf("必剪ASR创建任务失败: %w", err)
	}

	// 查询结果阶段
	if callback != nil {
		callback(60, "等待结果...")
	}
	result, err := b.queryResult(ctx, callback)
	if err != nil {
		Error("[%s] 查询结果失败: %v", instanceID, err)
		return nil, fmt.Errorf("必剪ASR查询结果失败: %w", err)
	}

	// 处理结果
	segments := b.makeSegments(result)

	if callback != nil {
		callback(100, "识别完成")
	}

	// 保存缓存
	if b.UseCache && len(segments) > 0 {
		if err := b.SaveToCache("./cache", cacheKey, segments); err != nil {
			Warn("[%s] 保存必剪ASR结果到缓存失败: %v", instanceID, err)
		}
	}

	return segments, nil
}

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

	// 使用带代理的客户端
	client := getHTTPClient()
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
		return fmt.Errorf("响应格式错误: 缺少data字段")
	}

	if inBossKey, ok := data["in_boss_key"].(string); ok {
		b.inBossKey = inBossKey
	}

	if resourceID, ok := data["resource_id"].(string); ok {
		b.resourceID = resourceID
	}

	if uploadID, ok := data["upload_id"].(string); ok {
		b.uploadID = uploadID
	}

	if perSize, ok := data["per_size"].(float64); ok {
		b.perSize = int(perSize)
	}

	uploadURLsIface, ok := data["upload_urls"].([]interface{})
	if !ok {
		return fmt.Errorf("upload_urls字段缺失或类型错误")
	}

	b.uploadURLs = make([]string, len(uploadURLsIface))
	for i, url := range uploadURLsIface {
		if urlStr, ok := url.(string); ok {
			b.uploadURLs[i] = urlStr
		}
	}

	b.clips = len(b.uploadURLs)
	Info("申请上传成功, 总计大小%dKB, %d分片, 分片大小%dKB", len(b.FileBinary)/1024, b.clips, b.perSize/1024)
	return nil
}

func (b *BcutASR) uploadParts() error {
	b.etags = make([]string, b.clips)
	// 使用带代理的客户端
	client := getHTTPClient()

	for i := 0; i < b.clips; i++ {
		startRange := i * b.perSize
		endRange := (i + 1) * b.perSize
		if endRange > len(b.FileBinary) {
			endRange = len(b.FileBinary)
		}

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

	// 使用带代理的客户端
	client := getHTTPClient()
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
	}

	Info("提交成功，获取下载URL: %s", b.downloadURL)
	return nil
}

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

	// 使用带代理的客户端
	client := getHTTPClient()
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
	}

	Info("任务已创建: %s", b.taskID)
	return nil
}

func (b *BcutASR) queryResult(ctx context.Context, callback ProgressCallback) (map[string]interface{}, error) {
	// 使用带代理的客户端
	client := getHTTPClient()
	instanceID := GenerateRandomString(6)

	Info("[BcutASR-%s] 开始轮询查询任务: %s", instanceID, b.taskID)

	for i := 0; i < MaxRetries; i++ {
		select {
		case <-ctx.Done():
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

		if state == 4 {
			resultStr, ok := data["result"].(string)
			if !ok || resultStr == "" {
				return nil, fmt.Errorf("任务完成但结果为空")
			}

			var resultData map[string]interface{}
			if err := json.Unmarshal([]byte(resultStr), &resultData); err != nil {
				return nil, fmt.Errorf("解析结果失败: %w", err)
			}
			return resultData, nil
		} else if state == 3 {
			return nil, fmt.Errorf("任务处理失败，状态: %v", state)
		}

		if callback != nil && i%5 == 0 {
			progress := 60 + int(float64(i)/float64(MaxRetries)*39)
			if progress > 99 {
				progress = 99
			}
			callback(progress, fmt.Sprintf("处理中 %d%%...", progress))
		}

		sleepDuration := RetryBaseDelay
		if i > 20 {
			sleepDuration = RetryBaseDelay * 2
		}
		if i > 50 {
			sleepDuration = RetryLongDelay
		}
		time.Sleep(sleepDuration)
	}

	return nil, fmt.Errorf("任务超时未完成")
}

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

// ==================== SRT生成 ====================

func formatSRTTime(seconds float64) string {
	h := int(seconds / 3600)
	m := int((seconds - float64(h*3600)) / 60)
	s := int(seconds - float64(h*3600) - float64(m*60))
	ms := int((seconds - float64(int(seconds))) * 1000)

	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, ms)
}

func generateSRT(segments []DataSegment) string {
	var srtBuffer bytes.Buffer

	for i, segment := range segments {
		srtBuffer.WriteString(fmt.Sprintf("%d\n", i+1))
		srtBuffer.WriteString(fmt.Sprintf("%s --> %s\n",
			formatSRTTime(segment.StartTime),
			formatSRTTime(segment.EndTime)))
		srtBuffer.WriteString(fmt.Sprintf("%s\n\n", segment.Text))
	}

	return srtBuffer.String()
}

func saveSRTFile(srtContent string, outputPath string) error {
	err := os.WriteFile(outputPath, []byte(srtContent), 0644)
	if err != nil {
		return fmt.Errorf("保存SRT文件失败: %w", err)
	}
	return nil
}

// ==================== AI总结服务 ====================

// AISummarizer AI总结器
type AISummarizer struct {
	config AIConfig
}

func NewAISummarizer(config AIConfig) *AISummarizer {
	return &AISummarizer{config: config}
}

// Summarize 调用AI进行总结
func (ai *AISummarizer) Summarize(req AIRequest) (AIResponse, error) {
	// 构建完整的文本内容（带时间戳，方便AI定位）
	var fullTextBuilder bytes.Buffer
	if len(req.Segments) > 0 {
		for _, seg := range req.Segments {
			// 格式：[12.5s] 这是一段话。
			fullTextBuilder.WriteString(fmt.Sprintf("[%.2fs] %s\n", seg.StartTime, seg.Text))
		}
	} else {
		fullTextBuilder.WriteString(req.Text)
	}
	fullText := fullTextBuilder.String()

	// 构建高质量 Prompt
	prompt := ai.config.CustomPrompt
	if prompt == "" {
		prompt = `你是一个专业的视频内容分析师。请根据提供的视频字幕内容（包含时间戳），生成一份高质量的图文总结。

要求：
1. **深度解析**：不要只列流水账，要提炼核心观点、逻辑脉络和关键细节。
2. **结构清晰**：使用 Markdown 的一级标题、二级标题、列表、引用等语法，排版美观。
3. **智能配图与时间定位**：
   - 在总结关键画面时，插入截图标记：[[CAPTURE: 秒数]]
   - **重要**：在每个核心观点、段落或列表项的开头，必须插入时间戳标记：[[TIME: 秒数]]，以便用户点击跳转和高亮阅读。
   - 例如：[[TIME: 15.5]] 本段主要讲述了... [[CAPTURE: 15.5]]
4. **语言风格**：专业、简洁、流畅，去除口语废话。

请开始总结：`
	}

	// 如果有截图，提及截图
	if len(req.Screenshots) > 0 {
		 fmt.Sprintf("\n注意：视频截图已保存在：%s，这些截图可以作为要点的视觉参考",
			strings.Join(req.Screenshots, ", "))
	}

	// 完整的prompt
	fullPrompt := fmt.Sprintf("%s\n\n内容：\n%s", prompt, fullText)

	// 如果没有配置API Key，使用本地模拟
	if ai.config.APIKey == "" {
		return ai.localSummarize(req.Text, req.Screenshots)
	}

	// 设置默认值
	if ai.config.APIURL == "" {
		ai.config.APIURL = "https://api.xiaomimimo.com/v1/chat/completions"
	}
	if ai.config.Model == "" {
		ai.config.Model = "mimo-v2-flash"
	}

	// 1. 调用 AI 获取包含标记的 Markdown
	rawResponse, err := ai.callExternalAI(fullPrompt, nil)
	if err != nil {
		Error("AI总结请求失败: %v", err) // 新增日志
		return rawResponse, err
	}

	// 2. 处理截图标记 [[CAPTURE: 123.45]]
	if req.VideoPath != "" {
		processedMarkdown, err := ai.processScreenshots(rawResponse.Markdown, req.VideoPath)
		if err == nil {
			rawResponse.Markdown = processedMarkdown
		} else {
			Warn("处理AI截图失败: %v", err)
		}
	}

	// 3. 保存总结结果到本地缓存
	if req.VideoPath != "" {
		vp, err := NewVideoProcessor(req.VideoPath)
		if err == nil {
			summaryPath := filepath.Join(vp.OutputDir, "summary.json")
			if data, err := json.MarshalIndent(rawResponse, "", "  "); err == nil {
				os.WriteFile(summaryPath, data, 0644)
				Info("AI总结已保存到: %s", summaryPath)
			}
		}
	}

	return rawResponse, nil
}

// processScreenshots 解析Markdown中的截图标记并生成图片
func (ai *AISummarizer) processScreenshots(markdown string, videoPath string) (string, error) {
	vp, err := NewVideoProcessor(videoPath)
	if err != nil {
		return markdown, err
	}

	// 正则匹配 [[CAPTURE: 123.45]]
	// 简单起见，我们逐行处理或使用 ReplaceAllStringFunc
	lines := strings.Split(markdown, "\n")
	var newLines []string

	for _, line := range lines {
		if strings.Contains(line, "[[CAPTURE:") {
			// 提取时间戳
			start := strings.Index(line, "[[CAPTURE:")
			end := strings.Index(line[start:], "]]")
			if end != -1 {
				tagContent := line[start+10 : start+end]
				seconds, err := strconv.ParseFloat(strings.TrimSpace(tagContent), 64)
				if err == nil {
					// 提取截图
					imgPath, err := vp.ExtractScreenshotAt(seconds)
					if err == nil {
						// 转换路径为 Web 可访问路径
						// 假设 DOWNLOAD_DIR 是 D:/download
						// imgPath 是 D:/download/output_xxx/ai_capture_xxx.jpg
						// Web路径应为 /files/output_xxx/ai_capture_xxx.jpg
						relPath, _ := filepath.Rel(DOWNLOAD_DIR, imgPath)
						webPath := "/files/" + filepath.ToSlash(relPath)

						// 替换标记为 Markdown 图片
						imageMd := fmt.Sprintf("\n![视频截图 %.2fs](%s)\n", seconds, webPath)
						line = strings.Replace(line, fmt.Sprintf("[[CAPTURE:%s]]", tagContent), imageMd, -1)
						// 同时也处理带空格的情况
						line = strings.Replace(line, fmt.Sprintf("[[CAPTURE: %s]]", tagContent), imageMd, -1)
					}
				}
			}
		}
		newLines = append(newLines, line)
	}

	return strings.Join(newLines, "\n"), nil
}

// localSummarize 本地模拟总结
func (ai *AISummarizer) localSummarize(text string, screenshots []string) (AIResponse, error) {
	// 分割文本，提取关键句子
	sentences := strings.Split(text, "。")
	var points []string

	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if len(sentence) > 10 {
			points = append(points, sentence)
			if len(points) >= 5 {
				break
			}
		}
	}

	if len(points) == 0 && len(text) > 0 {
		points = append(points, text)
	}

	// 构建Markdown输出
	var markdown bytes.Buffer
	markdown.WriteString("# 视频总结\n\n")

	screenshotInfo := ""
	if len(screenshots) > 0 {
		// 只显示截图文件名，不显示完整路径
		var screenshotNames []string
		for _, s := range screenshots {
			screenshotNames = append(screenshotNames, filepath.Base(s))
		}
		screenshotInfo = fmt.Sprintf("\n> 视频截图：%s", strings.Join(screenshotNames, ", "))
	}

	markdown.WriteString(fmt.Sprintf("## 核心要点%s\n\n", screenshotInfo))
	for i, point := range points {
		markdown.WriteString(fmt.Sprintf("- **要点%d**：%s\n", i+1, point))
	}

	markdown.WriteString("\n## 详细总结\n\n")
	markdown.WriteString(text)

	return AIResponse{
		Summary:  strings.Join(points, "；"),
		Markdown: markdown.String(),
		Points:   points,
		Success:  true,
	}, nil
}

// Chat 进行AI对话
func (ai *AISummarizer) Chat(req ChatRequest) (string, error) {
	// 设置默认值
	if ai.config.APIURL == "" {
		ai.config.APIURL = "https://api.xiaomimimo.com/v1/chat/completions"
	}
	if ai.config.Model == "" {
		ai.config.Model = "mimo-v2-flash"
	}

	// 构建消息列表
	var messages []map[string]string

	// 系统提示词 (更新)
	systemPrompt := `你是一位专业的AI导师。用户会根据一段视频的内容向你提问。
请基于提供的[上下文内容]回答用户的问题。
回答要求：
1. 像老师一样循循善诱，解答疑惑，语言通俗易懂。
2. **非常重要**：在回答的最后，请根据上下文内容出3道相关的课后选择题或思考题，帮助用户巩固知识。
3. 如果上下文中没有答案，请利用你的通用知识回答，并说明这一点。`

	if req.Context != "" {
		systemPrompt += fmt.Sprintf("\n\n[上下文内容]：\n%s", req.Context)
	}

	messages = append(messages, map[string]string{"role": "system", "content": systemPrompt})

	// 添加历史记录
	if len(req.History) > 0 {
		messages = append(messages, req.History...)
	}

	// 添加当前问题
	messages = append(messages, map[string]string{"role": "user", "content": req.Message})

	// 发送请求
	return ai.sendChatRequest(messages)
}

// sendChatRequest 发送通用聊天请求
func (ai *AISummarizer) sendChatRequest(messages []map[string]string) (string, error) {
	reqBody := map[string]interface{}{
		"model":    ai.config.Model,
		"messages": messages,
		"stream":   false,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("JSON编码失败: %w", err)
	}

	req, err := http.NewRequest("POST", ai.config.APIURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+ai.config.APIKey)

	// 使用带代理的客户端 (如果需要AI也走代理，可以使用 getHTTPClient，这里暂时保持直连或根据需求修改)
	// 考虑到国内访问OpenAI等可能需要代理，这里也统一使用代理
	client := getHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("API请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API错误 (状态码 %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}

	if len(result.Choices) == 0 {
		if result.Error.Message != "" {
			return "", fmt.Errorf("API返回错误: %s", result.Error.Message)
		}
		return "", fmt.Errorf("API返回结果为空")
	}

	return result.Choices[0].Message.Content, nil
}

// callExternalAI 调用外部AI (重构为使用 sendChatRequest)
func (ai *AISummarizer) callExternalAI(prompt string, screenshots []string) (AIResponse, error) {
	messages := []map[string]string{
		{"role": "user", "content": prompt},
	}

	content, err := ai.sendChatRequest(messages)
	if err != nil {
		return AIResponse{}, err
	}

	// 简单提取要点 (保持原有逻辑)
	var points []string
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "1. ") {
			cleanLine := strings.TrimLeft(line, "-*1234567890. ")
			if len(cleanLine) > 0 {
				points = append(points, cleanLine)
			}
		}
	}

	if len(points) == 0 {
		sentences := strings.Split(content, "。")
		for i, s := range sentences {
			if i >= 5 {
				break
			}
			if len(s) > 5 {
				points = append(points, s)
			}
		}
	}

	return AIResponse{
		Summary:  "AI智能总结",
		Markdown: content,
		Points:   points,
		Success:  true,
	}, nil
}

// ==================== 文件操作服务 ====================

// listDownloadFiles 列出D:/download目录下的文件
func listDownloadFiles() ([]FileItem, error) {
	var files []FileItem
	
	// 监听列表：主目录 和 dest子目录
	scanDirs := []string{DOWNLOAD_DIR, filepath.Join(DOWNLOAD_DIR, "dest")}

	for _, dir := range scanDirs {
		// 检查目录是否存在
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}

		// 读取目录内容
		entries, err := os.ReadDir(dir)
		if err != nil {
			Warn("读取目录失败 %s: %v", dir, err)
			continue
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			info, err := entry.Info()
			if err != nil {
				continue
			}

			fileType := "other"
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			if ext == ".mp4" || ext == ".avi" || ext == ".mkv" || ext == ".mov" || ext == ".flv" {
				fileType = "video"
			} else if ext == ".mp3" || ext == ".wav" || ext == ".flac" || ext == ".aac" {
				fileType = "audio"
			}

			// 过滤掉非视频音频文件
			if fileType == "other" {
				continue
			}

			files = append(files, FileItem{
				Name:    entry.Name(),
				Path:    filepath.Join(dir, entry.Name()), // 使用完整路径
				Size:    info.Size(),
				ModTime: info.ModTime().Format("2006-01-02 15:04:05"),
				Type:    fileType,
			})
		}
	}

	// 扫描归档目录
	archiveDir := filepath.Join(DOWNLOAD_DIR, "archive")
	if entries, err := os.ReadDir(archiveDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				// 检查是否存在 summary.json
				summaryPath := filepath.Join(archiveDir, entry.Name(), "summary.json")
				if _, err := os.Stat(summaryPath); err == nil {
					// 这是一个有效的归档
					name := entry.Name()
					// 去除 output_ 前缀，让名字更好看
					if strings.HasPrefix(name, "output_") {
						name = name[7:]
					}
					
					files = append(files, FileItem{
						Name:    "📦 [归档] " + name,
						Path:    filepath.Join(archiveDir, entry.Name()),
						Type:    "archive",
						ModTime: "已归档",
					})
				}
			}
		}
	}

	return files, nil
}

// ==================== HTTP服务 ====================

type HTTPServer struct {
	port      string
	aiConfig  AIConfig
}

func NewHTTPServer(port string) *HTTPServer {
	// 检查环境变量中的 API Key
	config := AIConfig{}
	if envKey := os.Getenv("ANTHROPIC_API_KEY"); envKey != "" {
		config.APIKey = envKey
		Info("已从环境变量加载 ANTHROPIC_API_KEY")
	}

	return &HTTPServer{
		port:     port,
		aiConfig: config,
	}
}

func (s *HTTPServer) Start() {
	// API路由
	http.HandleFunc("/api/list-files", s.handleListFiles)
	http.HandleFunc("/api/process-video", s.handleProcessVideo)
	http.HandleFunc("/api/delete-output", s.handleDeleteOutput)
	http.HandleFunc("/api/get-archive", s.handleGetArchive) // 新增：获取归档内容
	http.HandleFunc("/api/ai-summarize", s.handleAISummarize)
	http.HandleFunc("/api/ai-chat", s.handleAIChat)
	http.HandleFunc("/api/config", s.handleConfig)
	http.HandleFunc("/api/health", s.handleHealth)

	// 静态文件服务 (前端页面)
	http.Handle("/", http.FileServer(http.Dir("./static")))

	// 文件下载服务 (用于展示图片和下载结果)
	// 映射 /files/ -> D:/download/
	http.Handle("/files/", http.StripPrefix("/files/", http.FileServer(http.Dir(DOWNLOAD_DIR))))

	Info("HTTP服务启动在端口: %s", s.port)
	// localhost访问
	Info("访问地址: http://localhost:%s/", s.port)
	Info("静态文件目录: ./static")
	Info("下载目录: %s (映射到 /files/)", DOWNLOAD_DIR)

	err := http.ListenAndServe(":"+s.port, nil)
	if err != nil {
		Error("HTTP服务启动失败: %v", err)
	}
}

// handleListFiles 列出下载目录文件
func (s *HTTPServer) handleListFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "只支持GET方法", http.StatusMethodNotAllowed)
		return
	}

	files, err := listDownloadFiles()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"files":   files,
		"dir":     DOWNLOAD_DIR,
	})
}

// handleGetArchive 获取归档内容
func (s *HTTPServer) handleGetArchive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "只支持POST方法", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "解析请求失败", http.StatusBadRequest)
		return
	}

	summaryPath := filepath.Join(req.Path, "summary.json")
	data, err := os.ReadFile(summaryPath)
	if err != nil {
		http.Error(w, "读取归档失败: "+err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// handleProcessVideo 处理视频：提取音频 + ASR + SRT + 截图
func (s *HTTPServer) handleProcessVideo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "只支持POST方法", http.StatusMethodNotAllowed)
		return
	}

	decoder := json.NewDecoder(r.Body)
	var req ProcessRequest
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, "解析请求失败: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.VideoPath == "" {
		http.Error(w, "缺少video_path参数", http.StatusBadRequest)
		return
	}

	// 检查文件是否存在
	if _, err := os.Stat(req.VideoPath); os.IsNotExist(err) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ProcessResponse{
			Success: false,
			Message: "视频文件不存在",
		})
		return
	}

	// 处理视频
	vp, err := NewVideoProcessor(req.VideoPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// === 缓存检查开始 ===
	// 1. 检查是否存在 segments.json (ASR结果)
	segmentsPath := filepath.Join(vp.OutputDir, "segments.json")
	var segments []DataSegment
	segmentsLoaded := false

	if data, err := os.ReadFile(segmentsPath); err == nil {
		if json.Unmarshal(data, &segments) == nil && len(segments) > 0 {
			Info("从缓存加载ASR结果: %s", segmentsPath)
			segmentsLoaded = true
		}
	}

	// 2. 检查是否存在 summary.json (AI总结结果)
	summaryPath := filepath.Join(vp.OutputDir, "summary.json")
	var aiResult *AIResponse
	if data, err := os.ReadFile(summaryPath); err == nil {
		var res AIResponse
		if json.Unmarshal(data, &res) == nil {
			Info("从缓存加载AI总结: %s", summaryPath)
			aiResult = &res
		}
	}
	// === 缓存检查结束 ===

	// 如果是仅检查模式
	if req.CheckOnly {
		if segmentsLoaded {
			// 返回缓存数据
			result := ProcessResponse{
				Success:      true,
				Segments:     segments,
				OutputDir:    vp.OutputDir,
				SegmentCount: len(segments),
				AIResult:     aiResult,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(result)
			return
		} else {
			// 未处理
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(ProcessResponse{
				Success: false,
				Message: "未处理",
			})
			return
		}
	}

	var audioPath string
	var duration float64

	// 如果没有缓存，才进行音频提取和ASR
	if !segmentsLoaded {
		// 提取音频 (内部已实现存在检查)
		audioPath, err = vp.ExtractAudio()
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(ProcessResponse{
				Success: false,
				Message: "提取音频失败: " + err.Error(),
			})
			return
		}
		// 移除：不再自动删除音频文件，以便复用
		// defer func() {
		// 	Info("清理临时音频文件: %s", audioPath)
		// 	os.Remove(audioPath)
		// }()

		// ASR识别 - 禁用内部缓存，使用我们自己的文件缓存
		asrClient, err := NewBcutASR(audioPath, false)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(ProcessResponse{
				Success: false,
				Message: "创建ASR服务失败: " + err.Error(),
			})
			return
		}

		ctx := context.Background()
		segments, err = asrClient.GetResult(ctx, func(percent int, message string) {
			Info("ASR进度: %d%% - %s", percent, message)
		})
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(ProcessResponse{
				Success: false,
				Message: "ASR识别失败: " + err.Error(),
			})
			return
		}

		// 保存 segments.json
		if data, err := json.MarshalIndent(segments, "", "  "); err == nil {
			os.WriteFile(segmentsPath, data, 0644)
		}
	} else {
		// 如果加载了缓存，音频路径可能为空，但这不影响后续逻辑
		audioPath = filepath.Join(vp.OutputDir, "audio.mp3") // 假路径
	}

	// 提取视频时长 (总是尝试获取，很快)
	duration, err = vp.GetVideoDuration()
	if err != nil {
		duration = 0
	}

	// 生成SRT (总是重新生成或覆盖，很快)
	srtContent := generateSRT(segments)
	srtPath := filepath.Join(vp.OutputDir, "subtitles.srt")
	saveSRTFile(srtContent, srtPath)

	// 返回结果
	result := ProcessResponse{
		Success:      true,
		AudioPath:    audioPath,
		SrtPath:      srtPath,
		SrtContent:   srtContent,
		Segments:     segments,
		Screenshots:  []string{}, // 不再返回预设截图
		OutputDir:    vp.OutputDir,
		Duration:     duration,
		SegmentCount: len(segments),
		AIResult:     aiResult, // 返回缓存的AI结果
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleDeleteOutput 删除输出目录
func (s *HTTPServer) handleDeleteOutput(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "只支持POST方法", http.StatusMethodNotAllowed)
		return
	}

	var req ProcessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "解析请求失败", http.StatusBadRequest)
		return
	}

	vp, err := NewVideoProcessor(req.VideoPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// 调用新的归档并清理方法
	if err := vp.ArchiveAndClean(); err != nil {
		http.Error(w, "删除/归档失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// handleAISummarize 处理AI总结
func (s *HTTPServer) handleAISummarize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "只支持POST方法", http.StatusMethodNotAllowed)
		return
	}

	decoder := json.NewDecoder(r.Body)
	var req AIRequest
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, "解析请求失败: "+err.Error(), http.StatusBadRequest)
		return
	}

	aiSummarizer := NewAISummarizer(s.aiConfig)
	response, err := aiSummarizer.Summarize(req)
	if err != nil {
		http.Error(w, "AI总结失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleAIChat 处理AI对话
func (s *HTTPServer) handleAIChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "只支持POST方法", http.StatusMethodNotAllowed)
		return
	}

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "解析请求失败", http.StatusBadRequest)
		return
	}

	aiSummarizer := NewAISummarizer(s.aiConfig)
	reply, err := aiSummarizer.Chat(req)
	if err != nil {
		http.Error(w, "AI对话失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"reply":   reply,
	})
}

// handleConfig 处理AI配置
func (s *HTTPServer) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		decoder := json.NewDecoder(r.Body)
		var config AIConfig
		if err := decoder.Decode(&config); err != nil {
			http.Error(w, "解析配置失败: "+err.Error(), http.StatusBadRequest)
			return
		}
		s.aiConfig = config
		Info("AI配置更新: APIURL=%s, Model=%s", config.APIURL, config.Model)

		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "配置更新成功",
			"config":  config,
		})
		return
	}

	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"config":  s.aiConfig,
		})
		return
	}

	http.Error(w, "只支持GET或POST方法", http.StatusMethodNotAllowed)
}

func (s *HTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now(),
	})
}

// ==================== 主程序 ====================

func main() {
	mode := flag.String("mode", "server", "运行模式: cli 或 server")

	// CLI参数
	audioFile := flag.String("audio", "", "音频文件路径")
	videoFile := flag.String("video", "", "视频文件路径(用于提取音频)")
	useCache := flag.Bool("cache", true, "是否使用缓存")
	timeout := flag.Int("timeout", 300, "超时时间(秒)")

	// Server参数
	port := flag.String("port", HTTP_PORT, "HTTP服务端口")

	flag.Parse()

	if *mode == "server" {
		// 创建static目录
		os.MkdirAll("static", 0755)

		// 启动HTTP服务
		server := NewHTTPServer(*port)
		server.Start()
		return
	}

	// CLI模式
	if *audioFile == "" && *videoFile == "" {
		fmt.Println("=== 视频字幕生成与AI总结工具 ===")
		fmt.Println("\n使用方法:")
		fmt.Println("  CLI模式: go run main.go -mode cli -video <视频路径> [-cache true/false]")
		fmt.Println("  HTTP模式: go run main.go -mode server -port 8080")
		fmt.Println("\n示例:")
		fmt.Println("  go run main.go -mode cli -video D:/download/demo.mp4")
		fmt.Println("  go run main.go -mode server -port 8080")
		fmt.Println("\n功能说明:")
		fmt.Println("  - 视频处理：提取音频 + ASR识别 + SRT字幕生成 + 视频截图")
		fmt.Println("  - AI总结：支持自定义Prompt和API配置")
		fmt.Println("  - Web界面：通过浏览器访问 http://localhost:8080")
		fmt.Println("  - 文件管理：自动读取 D:/download 目录")
		return
	}

	fmt.Println("=== 视频字幕生成CLI工具 ===")

	// 视频处理流程
	if *videoFile != "" {
		fmt.Printf("视频文件: %s\n", *videoFile)

		// 创建视频处理器
		vp, err := NewVideoProcessor(*videoFile)
		if err != nil {
			log.Fatalf("创建视频处理器失败: %v", err)
		}

		// 提取音频
		fmt.Println("\n[1/4] 提取音频...")
		audioPath, err := vp.ExtractAudio()
		if err != nil {
			log.Fatalf("提取音频失败: %v", err)
		}
		fmt.Printf("音频提取成功: %s\n", audioPath)

		// 获取视频时长
		duration, err := vp.GetVideoDuration()
		if err != nil {
			Warn("获取视频时长失败: %v", err)
		} else {
			fmt.Printf("视频时长: %.2f 秒 (%.2f 分钟)\n", duration, duration/60)
		}

		// 提取截图
		fmt.Println("\n[2/4] 提取视频截图...")
		screenshots, err := vp.ExtractScreenshots(duration)
		if err != nil {
			Warn("提取截图失败: %v", err)
		} else {
			fmt.Printf("提取 %d 张截图成功\n", len(screenshots))
			for _, shot := range screenshots {
				fmt.Printf("  - %s\n", shot)
			}
		}

		// ASR识别
		fmt.Println("\n[3/4] 语音识别(ASR)...")
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeout)*time.Second)
		defer cancel()

		asrClient, err := NewBcutASR(audioPath, *useCache)
		if err != nil {
			log.Fatalf("创建ASR服务失败: %v", err)
		}

		progressCallback := func(percent int, message string) {
			fmt.Printf("\r进度: [%-40s] %d%% %s",
				strings.Repeat("=", percent/2), percent, message)
		}

		startTime := time.Now()
		segments, err := asrClient.GetResult(ctx, progressCallback)
		if err != nil {
			log.Fatalf("\nASR识别失败: %v", err)
		}

		fmt.Printf("\n\n✅ ASR完成！耗时: %.2f秒\n", time.Since(startTime).Seconds())
		fmt.Printf("识别结果: %d 段\n", len(segments))

		// 生成SRT
		fmt.Println("\n[4/4] 生成SRT字幕...")
		srtContent := generateSRT(segments)
		srtPath := filepath.Join(vp.OutputDir, "subtitles.srt")
		if err := saveSRTFile(srtContent, srtPath); err != nil {
			log.Fatalf("保存SRT失败: %v", err)
		}
		fmt.Printf("SRT字幕保存成功: %s\n", srtPath)

		// 保存JSON结果
		jsonPath := filepath.Join(vp.OutputDir, "segments.json")
		if saveResultsToFile(segments, jsonPath) {
			fmt.Printf("JSON结果保存成功: %s\n", jsonPath)
		}

		// 显示预览
		fmt.Println("\n=== 字幕预览 ===")
		for i, seg := range segments {
			if i >= 5 {
				fmt.Printf("... (共 %d 段)\n", len(segments))
				break
			}
			fmt.Printf("[%d] %.2f-%.2fs: %s\n", i+1, seg.StartTime, seg.EndTime, seg.Text)
		}

		fmt.Println("\n=== 处理完成 ===")
		fmt.Printf("输出目录: %s\n", vp.OutputDir)
		fmt.Println("文件列表:")
		fmt.Printf("  - audio.mp3 (音频)\n")
		fmt.Printf("  - subtitles.srt (字幕)\n")
		fmt.Printf("  - segments.json (JSON数据)\n")
		fmt.Printf("  - screenshot_*.jpg (截图)\n")
	} else if *audioFile != "" {
		// 仅处理音频（原有功能）
		fmt.Printf("音频文件: %s\n", *audioFile)

		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeout)*time.Second)
		defer cancel()

		asrClient, err := NewBcutASR(*audioFile, *useCache)
		if err != nil {
			log.Fatalf("创建ASR服务失败: %v", err)
		}

		progressCallback := func(percent int, message string) {
			fmt.Printf("\r进度: [%-40s] %d%% %s",
				strings.Repeat("=", percent/2), percent, message)
		}

		startTime := time.Now()
		segments, err := asrClient.GetResult(ctx, progressCallback)
		if err != nil {
			log.Fatalf("\n处理失败: %v", err)
		}

		fmt.Printf("\n\n✅ 识别完成！耗时: %.2f秒\n", time.Since(startTime).Seconds())
		fmt.Printf("识别结果共 %d 段:\n\n", len(segments))

		for i, segment := range segments {
			fmt.Printf("[%d] 时间: %.2f - %.2f秒\n", i+1, segment.StartTime, segment.EndTime)
			fmt.Printf("    内容: %s\n\n", segment.Text)
		}

		// 保存结果
		outputFileName := fmt.Sprintf("asr_result_%d.json", time.Now().Unix())
		saveResultsToFile(segments, outputFileName)
		fmt.Printf("结果已保存到: %s\n", outputFileName)
	}
}

// saveResultsToFile 保存JSON结果
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
