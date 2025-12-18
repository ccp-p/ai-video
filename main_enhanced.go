// 视频转字幕 - 完整后端服务
// 功能：视频音频提取 + 语音识别 + SRT字幕生成 + HTTP服务 + AI总结
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
	HTTP_PORT = "8080"
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
	APIKey    string `json:"api_key"`
	APIURL    string `json:"api_url"`
	Model     string `json:"model"`
	CustomPrompt string `json:"custom_prompt"`
}

// AIRequest AI请求
type AIRequest struct {
	Text         string    `json:"text"`
	Prompt       string    `json:"prompt"`
	Segments     []DataSegment `json:"segments"`
	Screenshots  []string  `json:"screenshots"`
	VideoPath    string    `json:"video_path"`
}

// AIResponse AI响应
type AIResponse struct {
	Summary string `json:"summary"`
	Markdown string `json:"markdown"`
	Points  []string `json:"points"`
	Success bool   `json:"success"`
}

// ProgressCallback 进度回调函数类型
type ProgressCallback func(percent int, message string)

// ==================== 工具函数 ====================

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

// ExtractAudio 从视频提取音频
func (vp *VideoProcessor) ExtractAudio() (string, error) {
	audioPath := filepath.Join(vp.OutputDir, "audio.mp3")

	cmd := exec.Command("ffmpeg", "-i", vp.VideoPath, "-vn", "-acodec", "libmp3lame",
		"-ac", "2", "-ar", "16000", "-y", audioPath)

	_, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("提取音频失败: %v", err)
	}

	Info("音频提取成功: %s", audioPath)
	return audioPath, nil
}

// ExtractScreenshots 提取视频截图
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
	client := &http.Client{Timeout: TimeoutSeconds * time.Second}

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
	}

	Info("任务已创建: %s", b.taskID)
	return nil
}

func (b *BcutASR) queryResult(ctx context.Context, callback ProgressCallback) (map[string]interface{}, error) {
	client := &http.Client{Timeout: TimeoutSeconds * time.Second}
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
	// 构建完整的文本内容
	var fullText string
	if len(req.Segments) > 0 {
		// 使用字幕内容
		for _, seg := range req.Segments {
			fullText += seg.Text + " "
		}
	} else {
		// 使用直接输入的文本
		fullText = req.Text
	}

	// 构建prompt
	prompt := ai.config.CustomPrompt
	if prompt == "" {
		prompt = "请详细总结以下内容，要求：\n1. 提炼核心要点\n2. 用Markdown格式输出\n3. 结构清晰，易于阅读"
	}

	// 如果有截图，提及截图
	screenshotInfo := ""
	if len(req.Screenshots) > 0 {
		screenshotInfo = fmt.Sprintf("\n注意：视频截图已保存在：%s，这些截图可以作为要点的视觉参考",
			strings.Join(req.Screenshots, ", "))
	}

	// 完整的prompt
	fullPrompt := fmt.Sprintf("%s\n\n内容：%s\n%s", prompt, fullText, screenshotInfo)

	// 如果没有配置API，使用本地模拟
	if ai.config.APIKey == "" || ai.config.APIURL == "" {
		return ai.localSummarize(fullText, req.Screenshots)
	}

	// 调用外部AI API - 简化的实现
	return ai.callExternalAI(fullPrompt, req.Screenshots)
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
		screenshotInfo = fmt.Sprintf("\n> 视频截图：%s", strings.Join(screenshots, ", "))
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

// callExternalAI 调用外部AI（简化版，实际使用需要完善）
func (ai *AISummarizer) callExternalAI(prompt string, screenshots []string) (AIResponse, error) {
	// 这里是AI API调用的占位符
	// 实际实现需要根据具体AI服务的API文档来完成
	// 例如OpenAI、文心一言、通义千问等

	// 为了演示，暂时返回本地结果
	return ai.localSummarize("", screenshots)
}

// ==================== HTTP服务 ====================

type HTTPServer struct {
	port       string
	videoProcessor *VideoProcessor
	asrClient  *BcutASR
	aiConfig   AIConfig
}

func NewHTTPServer(port string) *HTTPServer {
	return &HTTPServer{
		port: port,
		aiConfig: AIConfig{},
	}
}

func (s *HTTPServer) Start() {
	http.HandleFunc("/api/process-video", s.handleProcessVideo)
	http.HandleFunc("/api/ai-summarize", s.handleAISummarize)
	http.HandleFunc("/api/config", s.handleConfig)
	http.HandleFunc("/api/health", s.handleHealth)
	http.HandleFunc("/", s.handleWebUI)

	Info("HTTP服务启动在端口: %s", s.port)
	err := http.ListenAndServe(":"+s.port, nil)
	if err != nil {
		Error("HTTP服务启动失败: %v", err)
	}
}

// handleProcessVideo 处理视频：提取音频 + ASR + SRT + 截图
func (s *HTTPServer) handleProcessVideo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "只支持POST或GET方法", http.StatusMethodNotAllowed)
		return
	}

	// 获取视频路径（从查询参数或表单）
	var videoPath string
	if r.Method == http.MethodGet {
		videoPath = r.URL.Query().Get("video")
	} else {
		r.ParseMultipartForm(10 << 20) // 10MB限制
		videoPath = r.FormValue("video")
	}

	if videoPath == "" {
		http.Error(w, "缺少video参数", http.StatusBadRequest)
		return
	}

	// 检查文件是否存在
	if _, err := os.Stat(videoPath); os.IsNotExist(err) {
		http.Error(w, "视频文件不存在: "+videoPath, http.StatusBadRequest)
		return
	}

	// 处理视频
	vp, err := NewVideoProcessor(videoPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 提取音频
	audioPath, err := vp.ExtractAudio()
	if err != nil {
		http.Error(w, "提取音频失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 提取视频时长
	duration, err := vp.GetVideoDuration()
	if err != nil {
		duration = 0 // 继续处理
	}

	// 提取截图
	screenshots, err := vp.ExtractScreenshots(duration)
	if err != nil {
		Warn("提取截图失败: %v", err)
	}

	// ASR识别
	asrClient, err := NewBcutASR(audioPath, true)
	if err != nil {
		http.Error(w, "创建ASR服务失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	ctx := context.Background()
	segments, err := asrClient.GetResult(ctx, func(percent int, message string) {
		Info("ASR进度: %d%% - %s", percent, message)
	})
	if err != nil {
		http.Error(w, "ASR识别失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 生成SRT
	srtContent := generateSRT(segments)
	srtPath := filepath.Join(vp.OutputDir, "subtitles.srt")
	if err := saveSRTFile(srtContent, srtPath); err != nil {
		http.Error(w, "保存SRT失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 返回结果
	result := map[string]interface{}{
		"success":      true,
		"audio_path":   audioPath,
		"srt_path":     srtPath,
		"srt_content":  srtContent,
		"segments":     segments,
		"screenshots":  screenshots,
		"output_dir":   vp.OutputDir,
		"duration":     duration,
		"segment_count": len(segments),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
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
		"status": "healthy",
		"timestamp": time.Now(),
	})
}

// handleWebUI 提供Web界面
func (s *HTTPServer) handleWebUI(w http.ResponseWriter, r *http.Request) {
	html := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>视频字幕生成与AI总结工具</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body { font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; background: linear-gradient(135deg, #667eea 0%, #764ba2 100%); min-height: 100vh; padding: 20px; }
        .container { max-width: 1200px; margin: 0 auto; background: white; border-radius: 12px; box-shadow: 0 20px 60px rgba(0,0,0,0.3); overflow: hidden; }
        .header { background: linear-gradient(135deg, #667eea 0%, #764ba2 100%); color: white; padding: 30px; text-align: center; }
        .header h1 { font-size: 28px; margin-bottom: 10px; }
        .header p { opacity: 0.9; font-size: 14px; }
        .tabs { display: flex; background: #f8f9fa; border-bottom: 2px solid #e9ecef; }
        .tab { flex: 1; padding: 15px; text-align: center; cursor: pointer; border: none; background: transparent; font-size: 16px; transition: all 0.3s; }
        .tab:hover { background: #e9ecef; }
        .tab.active { background: white; color: #667eea; border-bottom: 3px solid #667eea; font-weight: bold; }
        .tab-content { display: none; padding: 25px; }
        .tab-content.active { display: block; }
        .form-group { margin-bottom: 20px; }
        .form-group label { display: block; margin-bottom: 8px; font-weight: 600; color: #333; }
        .form-group input[type="text"],
        .form-group input[type="file"],
        .form-group textarea { width: 100%; padding: 12px; border: 2px solid #e9ecef; border-radius: 6px; font-size: 14px; transition: border 0.3s; }
        .form-group input:focus, .form-group textarea:focus { border-color: #667eea; outline: none; }
        .form-group textarea { min-height: 120px; resize: vertical; font-family: monospace; }
        .btn { background: linear-gradient(135deg, #667eea 0%, #764ba2 100%); color: white; padding: 12px 24px; border: none; border-radius: 6px; cursor: pointer; font-size: 16px; font-weight: 600; width: 100%; transition: transform 0.2s; }
        .btn:hover { transform: translateY(-2px); }
        .btn:disabled { opacity: 0.6; cursor: not-allowed; transform: none; }
        .progress { margin: 15px 0; height: 8px; background: #e9ecef; border-radius: 4px; overflow: hidden; display: none; }
        .progress-bar { height: 100%; background: linear-gradient(90deg, #667eea, #764ba2); width: 0%; transition: width 0.3s; }
        .result { margin-top: 20px; padding: 20px; background: #f8f9fa; border-radius: 6px; border: 1px solid #e9ecef; max-height: 500px; overflow-y: auto; font-size: 14px; }
        .result pre, .result textarea { white-space: pre-wrap; word-wrap: break-word; background: white; padding: 15px; border-radius: 4px; border: 1px solid #dee2e6; }
        .result h3 { margin-top: 15px; color: #667eea; }
        .result ul { margin-left: 20px; margin-top: 8px; }
        .alert { padding: 12px; border-radius: 6px; margin: 10px 0; display: none; }
        .alert.success { background: #d4edda; color: #155724; border: 1px solid #c3e6cb; }
        .alert.error { background: #f8d7da; color: #721c24; border: 1px solid #f5c6cb; }
        .alert.info { background: #d1ecf1; color: #0c5460; border: 1px solid #bee5eb; }
        .code-block { background: #2d2d2d; color: #f8f8f2; padding: 15px; border-radius: 6px; overflow-x: auto; font-family: 'Courier New', monospace; font-size: 13px; }
        .config-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 15px; }
        @media (max-width: 768px) { .config-grid { grid-template-columns: 1fr; } }
        .loading { text-align: center; padding: 20px; color: #667eea; font-weight: 600; }
        .screenshot-info { background: #fff3cd; padding: 10px; border-radius: 4px; margin: 10px 0; border: 1px solid #ffeaa7; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>🎬 视频字幕生成与AI总结工具</h1>
            <p>FFmpeg + B站ASR + AI智能总结</p>
        </div>

        <div class="tabs">
            <button class="tab active" onclick="showTab('tab1')">视频处理</button>
            <button class="tab" onclick="showTab('tab2')">AI总结</button>
            <button class="tab" onclick="showTab('tab3')">AI配置</button>
        </div>

        <!-- 视频处理 Tab -->
        <div id="tab1" class="tab-content active">
            <div class="form-group">
                <label>视频文件路径：</label>
                <input type="text" id="videoPath" placeholder="例如：D:/videos/video.mp4" />
                <small style="color: #666;">请提供完整的视频文件路径（支持MP4, AVI, MKV等格式）</small>
            </div>
            <button class="btn" onclick="processVideo()">开始处理视频</button>

            <div class="progress" id="videoProgress">
                <div class="progress-bar" id="videoProgressBar"></div>
            </div>

            <div class="alert" id="videoAlert"></div>

            <div class="result" id="videoResult" style="display:none;">
                <h3>处理结果</h3>
                <div id="videoResultContent"></div>
            </div>
        </div>

        <!-- AI总结 Tab -->
        <div id="tab2" class="tab-content">
            <div class="form-group">
                <label>输入文本（也可上传SRT文件）：</label>
                <textarea id="aiText" placeholder="在此输入要总结的文本，或者上传SRT文件..."></textarea>
            </div>
            <div class="form-group">
                <label>或上传SRT/文本文件：</label>
                <input type="file" id="aiFile" accept=".srt,.txt" onchange="loadFileContent()" />
            </div>
            <div class="form-group">
                <label>自定义Prompt（可选）：</label>
                <textarea id="customPrompt" placeholder="例如：请提取以下内容的核心要点，用Markdown格式输出..."></textarea>
            </div>
            <div class="form-group">
                <label>视频截图路径（可选，多个用逗号分隔）：</label>
                <input type="text" id="screenshotPaths" placeholder="例如：D:/videos/output/screenshot_1.jpg, D:/videos/output/screenshot_2.jpg" />
            </div>
            <button class="btn" onclick="aiSummarize()">AI智能总结</button>

            <div class="alert" id="aiAlert"></div>

            <div class="result" id="aiResult" style="display:none;">
                <h3>AI总结结果</h3>
                <div id="aiResultContent"></div>
            </div>
        </div>

        <!-- AI配置 Tab -->
        <div id="tab3" class="tab-content">
            <div class="config-grid">
                <div class="form-group">
                    <label>API Key：</label>
                    <input type="text" id="apiKey" placeholder="输入您的API Key" />
                </div>
                <div class="form-group">
                    <label>API URL：</label>
                    <input type="text" id="apiUrl" placeholder="例如：https://api.openai.com/v1/chat/completions" />
                </div>
                <div class="form-group">
                    <label>模型名称：</label>
                    <input type="text" id="model" placeholder="例如：gpt-4, gpt-3.5-turbo" />
                </div>
                <div class="form-group">
                    <label>自定义Prompt：</label>
                    <textarea id="configPrompt" placeholder="默认总结提示词，留空使用内置模板"></textarea>
                </div>
            </div>
            <div class="form-group">
                <label>当前配置状态：</label>
                <div class="code-block" id="currentConfig">未配置，将使用本地总结</div>
            </div>
            <button class="btn" onclick="saveConfig()">保存配置</button>
            <div class="alert" id="configAlert"></div>
            <div style="margin-top: 15px; padding: 15px; background: #e7f3ff; border-radius: 6px; font-size: 13px;">
                <strong>提示：</strong>如果不配置API，系统会使用本地算法生成总结。配置API后可使用更强大的AI模型。
            </div>
        </div>
    </div>

    <script>
        // Tab切换
        function showTab(tabId) {
            document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
            document.querySelectorAll('.tab-content').forEach(c => c.classList.remove('active'));
            event.target.classList.add('active');
            document.getElementById(tabId).classList.add('active');
        }

        // 显示提示信息
        function showAlert(elementId, message, type) {
            const alert = document.getElementById(elementId);
            alert.className = 'alert ' + type;
            alert.textContent = message;
            alert.style.display = 'block';
            setTimeout(() => { alert.style.display = 'none'; }, 5000);
        }

        // 加载文件内容
        function loadFileContent() {
            const file = document.getElementById('aiFile').files[0];
            if (!file) return;

            const reader = new FileReader();
            reader.onload = function(e) {
                document.getElementById('aiText').value = e.target.result;
                showAlert('aiAlert', '文件内容已加载', 'success');
            };
            reader.readAsText(file);
        }

        // 处理视频
        async function processVideo() {
            const videoPath = document.getElementById('videoPath').value.trim();
            if (!videoPath) {
                showAlert('videoAlert', '请输入视频文件路径', 'error');
                return;
            }

            const btn = event.target;
            const progress = document.getElementById('videoProgress');
            const progressBar = document.getElementById('videoProgressBar');
            const result = document.getElementById('videoResult');

            btn.disabled = true;
            progress.style.display = 'block';
            result.style.display = 'none';

            // 模拟进度条
            let progressValue = 0;
            const interval = setInterval(() => {
                progressValue += 2;
                if (progressValue > 90) progressValue = 90;
                progressBar.style.width = progressValue + '%';
            }, 200);

            try {
                // 使用GET请求进行演示
                const response = await fetch('/api/process-video?video=' + encodeURIComponent(videoPath));
                const data = await response.json();

                clearInterval(interval);
                progressBar.style.width = '100%';

                if (data.success) {
                    showAlert('videoAlert', '处理完成！', 'success');

                    let content = "<p><strong>音频文件：</strong><br><code>" + data.audio_path + "</code></p>" +
                        "<p><strong>SRT字幕：</strong><br><code>" + data.srt_path + "</code></p>" +
                        "<p><strong>视频时长：</strong>" + (data.duration ? (data.duration/60).toFixed(2) + " 分钟" : "未知") + "</p>" +
                        "<p><strong>识别段数：</strong>" + data.segment_count + " 段</p>" +
                        "<p><strong>输出目录：</strong><br><code>" + data.output_dir + "</code></p>";

                    if (data.screenshots && data.screenshots.length > 0) {
                        content += "<div class='screenshot-info'><strong>提取的截图：</strong><br>" + data.screenshots.join('<br>') + "</div>";
                    }

                    content += "<h4>SRT预览：</h4><div class='code-block'>" +
                        (data.srt_content.substring(0, 500) + (data.srt_content.length > 500 ? "..." : "")) + "</div>";

                    document.getElementById('videoResultContent').innerHTML = content;
                    result.style.display = 'block';
                } else {
                    showAlert('videoAlert', '处理失败: ' + (data.message || '未知错误'), 'error');
                }
            } catch (error) {
                clearInterval(interval);
                showAlert('videoAlert', '请求失败: ' + error.message, 'error');
            } finally {
                btn.disabled = false;
                setTimeout(() => { progress.style.display = 'none'; }, 1000);
            }
        }

        // AI总结
        async function aiSummarize() {
            const text = document.getElementById('aiText').value.trim();
            const customPrompt = document.getElementById('customPrompt').value.trim();
            const screenshotPaths = document.getElementById('screenshotPaths').value.trim();

            if (!text) {
                showAlert('aiAlert', '请输入文本或上传文件', 'error');
                return;
            }

            const btn = event.target;
            btn.disabled = true;
            document.getElementById('aiResult').style.display = 'none';

            const screenshots = screenshotPaths ? screenshotPaths.split(',').map(s => s.trim()) : [];

            try {
                const response = await fetch('/api/ai-summarize', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        text: text,
                        prompt: customPrompt,
                        screenshots: screenshots
                    })
                });

                const data = await response.json();

                if (data.success) {
                    showAlert('aiAlert', '总结完成！', 'success');

                    let pointsList = "";
                    for (var i = 0; i < data.points.length; i++) {
                        pointsList += "<li>" + data.points[i] + "</li>";
                    }

                    let content = "<h4>核心要点：</h4><ul>" + pointsList + "</ul>" +
                        "<h4>完整内容：</h4><div class='code-block'>" + data.summary + "</div>" +
                        "<h4>Markdown格式：</h4><div class='code-block'>" +
                        data.markdown.replace(/</g, '<').replace(/>/g, '>') + "</div>";

                    document.getElementById('aiResultContent').innerHTML = content;
                    document.getElementById('aiResult').style.display = 'block';
                } else {
                    showAlert('aiAlert', '总结失败: ' + (data.message || '未知错误'), 'error');
                }
            } catch (error) {
                showAlert('aiAlert', '请求失败: ' + error.message, 'error');
            } finally {
                btn.disabled = false;
            }
        }

        // 保存配置
        async function saveConfig() {
            const apiKey = document.getElementById('apiKey').value.trim();
            const apiUrl = document.getElementById('apiUrl').value.trim();
            const model = document.getElementById('model').value.trim();
            const configPrompt = document.getElementById('configPrompt').value.trim();

            if (!apiKey || !apiUrl) {
                // 允许不配置，使用本地总结
            }

            try {
                const response = await fetch('/api/config', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        api_key: apiKey,
                        api_url: apiUrl,
                        model: model,
                        custom_prompt: configPrompt
                    })
                });

                const data = await response.json();

                if (data.success) {
                    showAlert('configAlert', '配置保存成功！', 'success');
                    updateConfigDisplay(data.config);
                } else {
                    showAlert('configAlert', '保存失败: ' + (data.message || '未知错误'), 'error');
                }
            } catch (error) {
                showAlert('configAlert', '请求失败: ' + error.message, 'error');
            }
        }

        // 更新配置显示
        function updateConfigDisplay(config) {
            const display = document.getElementById('currentConfig');
            if (config.api_key && config.api_url) {
                display.textContent = "API: " + config.api_url + "\nModel: " + (config.model || "default") + "\nStatus: 已配置";
                display.style.background = '#d4edda';
                display.style.color = '#155724';
            } else {
                display.textContent = '未配置，将使用本地总结';
                display.style.background = '#fff3cd';
                display.style.color = '#856404';
            }
        }

        // 页面加载时获取当前配置
        window.addEventListener('load', async function() {
            try {
                const response = await fetch('/api/config');
                const data = await response.json();
                if (data.success && data.config) {
                    const config = data.config;
                    document.getElementById('apiKey').value = config.api_key || '';
                    document.getElementById('apiUrl').value = config.api_url || '';
                    document.getElementById('model').value = config.model || '';
                    document.getElementById('configPrompt').value = config.custom_prompt || '';
                    updateConfigDisplay(config);
                }
            } catch (error) {
                console.log('无法获取配置:', error);
            }
        });
    </script>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

// ==================== 命令行工具 ====================

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

// ==================== 主程序 ====================

func main() {
	// 命令行模式
	mode := flag.String("mode", "cli", "运行模式: cli 或 server")

	// CLI参数
	audioFile := flag.String("audio", "", "音频文件路径")
	videoFile := flag.String("video", "", "视频文件路径(用于提取音频)")
	useCache := flag.Bool("cache", true, "是否使用缓存")
	timeout := flag.Int("timeout", 300, "超时时间(秒)")

	// Server参数
	port := flag.String("port", HTTP_PORT, "HTTP服务端口")

	flag.Parse()

	if *mode == "server" {
		// 启动HTTP服务
		server := NewHTTPServer(*port)
		server.Start()
		return
	}

	// CLI模式
	if *audioFile == "" && *videoFile == "" {
		fmt.Println("=== 视频字幕生成与AI总结工具 ===")
		fmt.Println("\n使用方法:")
		fmt.Println("  CLI模式: go run main_enhanced.go -mode cli -video <视频路径> [-cache true/false]")
		fmt.Println("  HTTP模式: go run main_enhanced.go -mode server -port 8080")
		fmt.Println("\n示例:")
		fmt.Println("  go run main_enhanced.go -mode cli -video D:/videos/demo.mp4")
		fmt.Println("  go run main_enhanced.go -mode server -port 8080")
		fmt.Println("\n功能说明:")
		fmt.Println("  - 视频处理：提取音频 + ASR识别 + SRT字幕生成 + 视频截图")
		fmt.Println("  - AI总结：支持自定义Prompt和API配置")
		fmt.Println("  - Web界面：通过浏览器访问 http://localhost:8080")
		return
	}

	fmt.Println("=== 视频字幕生成CLI工具 ===")

	// 视频处理流程
	if *videoFile != "" {
		fmt.Printf("视频文件: %s\\n", *videoFile)

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
		fmt.Printf("  - audiio.mp3 (音频)\n")
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
