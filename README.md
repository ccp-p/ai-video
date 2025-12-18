# 视频字幕生成与AI总结工具

一个完整的视频处理工具，使用Go语言编写，支持视频音频提取、语音识别、SRT字幕生成、视频截图和AI智能总结。

## 功能特性

### 后端功能
- ✅ **视频处理**：使用FFmpeg提取音频和视频截图
- ✅ **语音识别**：集成B站必剪ASR服务进行语音转文字
- ✅ **字幕生成**：自动生成SRT格式字幕文件
- ✅ **多格式支持**：支持MP4、AVI、MKV等常见视频格式
- ✅ **缓存系统**：避免重复识别，提高效率
- ✅ **HTTP服务**：提供REST API和Web界面

### 前端功能
- ✅ **纯HTML界面**：单文件HTML，无需构建工具
- ✅ **视频处理**：一键处理视频，提取音频+字幕+截图
- ✅ **AI总结**：支持文本输入和SRT文件上传
- ✅ **自定义配置**：可配置AI API Key、API URL和模型
- ✅ **Prompt自定义**：支持用户自定义总结提示词
- ✅ **Markdown输出**：结构化总结，带要点和截图引用

## 环境要求

### 必需依赖
1. **Go语言**：1.21或更高版本
2. **FFmpeg**：用于视频音频提取和截图
   - 下载地址：https://ffmpeg.org/download.html
   - 确保ffmpeg和ffprobe在系统PATH中

### 可选依赖
- **AI API Key**：用于AI总结（如OpenAI、文心一言、通义千问等）
- 不配置时使用本地算法生成总结

## 快速开始

### 1. 安装FFmpeg
确保已安装FFmpeg并添加到系统PATH：
```bash
# Windows: 下载后将bin目录添加到环境变量
# 验证安装
ffmpeg -version
ffprobe -version
```

### 2. 编译和运行

#### 编译
```bash
cd D:\project\ccode
go mod tidy
```

#### 运行模式

**HTTP服务模式（推荐）**：
```bash
go run main_enhanced.go -mode server -port 8080
```

**命令行模式**：
```bash
# 处理视频，提取音频、字幕和截图
go run main_enhanced.go -mode cli -video D:/videos/demo.mp4

# 仅处理音频
go run main_enhanced.go -mode cli -audio D:/videos/audio.mp3
```

### 3. 访问Web界面

打开浏览器访问：`http://localhost:8080`

界面包含三个标签页：
1. **视频处理**：输入视频路径，一键生成字幕
2. **AI总结**：输入文本或上传SRT，进行AI总结
3. **AI配置**：配置API Key、API URL和自定义Prompt

## 功能详细说明

### 视频处理流程
1. **音频提取**：从视频中提取音频为MP3格式
2. **视频截图**：按时间间隔自动提取5张关键帧截图
3. **语音识别**：调用B站必剪ASR服务进行语音转文字
4. **字幕生成**：生成SRT格式字幕文件
5. **结果保存**：在视频同目录下创建output文件夹，保存所有文件

### AI总结功能
- **文本输入**：直接输入文本或粘贴内容
- **文件上传**：支持上传SRT或TXT文件
- **自定义Prompt**：可定义AI总结的风格和要求
- **截图引用**：可以在总结中引用视频截图路径
- **Markdown输出**：结构化的总结报告，包含要点列表

### AI配置说明
- **API Key**：AI服务的访问密钥
- **API URL**：AI服务的API地址
  - 示例：`https://api.openai.com/v1/chat/completions`
- **模型名称**：使用的AI模型
  - 示例：`gpt-4`, `gpt-3.5-turbo`, `ernie-bot`等
- **自定义Prompt**：默认总结提示词

## API接口文档

### 1. 处理视频
```http
POST /api/process-video?video=D:/videos/demo.mp4
```

返回：
```json
{
  "success": true,
  "audio_path": "D:/videos/output_demo.mp4/audio.mp3",
  "srt_path": "D:/videos/output_demo.mp4/subtitles.srt",
  "srt_content": "1\n00:00:01,050 --> 00:00:03,150\n欢迎观看本视频\n...",
  "segments": [...],
  "screenshots": ["D:/videos/output_demo.mp4/screenshot_1.jpg", ...],
  "output_dir": "D:/videos/output_demo.mp4",
  "duration": 180.5,
  "segment_count": 15
}
```

### 2. AI总结
```http
POST /api/ai-summarize
Content-Type: application/json

{
  "text": "要总结的文本内容...",
  "prompt": "自定义提示词",
  "segments": [...],  // ASR结果
  "screenshots": ["path/to/screenshot.jpg"],
  "video_path": "path/to/video.mp4"
}
```

返回：
```json
{
  "success": true,
  "summary": "简短总结...",
  "markdown": "# 视频总结\n\n## 核心要点\n- 要点1\n- 要点2\n...",
  "points": ["要点1", "要点2", ...]
}
```

### 3. 配置管理
```http
# 获取配置
GET /api/config

# 设置配置
POST /api/config
Content-Type: application/json

{
  "api_key": "your-api-key",
  "api_url": "https://api.example.com/v1/chat/completions",
  "model": "gpt-4",
  "custom_prompt": "请详细总结以下内容..."
}
```

## 输出文件说明

处理视频后会在视频同目录创建 `output_<视频名>` 文件夹，包含：

- `audio.mp3` - 提取的音频文件
- `subtitles.srt` - SRT字幕文件
- `segments.json` - ASR识别结果（JSON格式）
- `screenshot_1.jpg` ~ `screenshot_5.jpg` - 视频截图

## 使用示例

### 示例1：处理视频并得到字幕
```bash
# 1. 启动HTTP服务
go run main_enhanced.go -mode server -port 8080

# 2. 浏览器访问 http://localhost:8080
# 3. 在"视频处理"标签页输入：D:/videos/我的视频.mp4
# 4. 等待处理完成，下载SRT字幕
```

### 示例2：AI总结现有文本
```bash
# 1. 在"AI总结"标签页
# 2. 粘贴文本或上传SRT文件
# 3. 配置AI API（可选，不配置使用本地算法）
# 4. 点击"AI智能总结"
# 5. 查看Markdown格式的总结报告
```

### 示例3：命令行处理
```bash
# 处理视频，自动生成字幕和截图
go run main_enhanced.go -mode cli -video "D:/videos/demo.mp4"

# 输出：
# === 视频字幕生成CLI工具 ===
# [1/4] 提取音频...
# [2/4] 提取视频截图...
# [3/4] 语音识别(ASR)...
# [4/4] 生成SRT字幕...
# 处理完成！文件保存在：D:/videos/output_demo.mp4/
```

## 常见问题

### Q: 提示"未找到ffmpeg"
A: 请下载FFmpeg并添加到系统PATH环境变量。

### Q: ASR识别失败
A: 可能是网络问题或B站API限制。建议：
1. 检查网络连接
2. 尝试缓存功能（默认开启）
3. 使用较短的音频测试

### Q: AI总结只有本地算法
A: 这是正常的。如果不配置API Key和API URL，系统会使用本地算法生成基础总结。配置后可使用更强大的AI模型。

### Q: 如何配置自定义AI服务
A: 在"AI配置"页面填写：
- API Key：你的API密钥
- API URL：AI服务的接口地址
- 模型：模型名称
- 自定义Prompt：总结的提示词

## 技术实现

- **Go 标准库**：http, json, os, exec等
- **FFmpeg**：音频提取和截图
- **B站必剪ASR**：语音识别API（来自Bilibili）
- **纯前端**：HTML+CSS+JavaScript，无依赖

## 注意事项

1. **网络依赖**：ASR服务需要网络连接
2. **文件大小**：大文件处理时间较长，请耐心等待
3. **浏览器兼容**：现代浏览器均支持（Chrome, Firefox, Edge等）
4. **安全说明**：配置的API Key仅存储在内存中

## 项目结构

```
D:\project\ccode\
├── main_enhanced.go     # 主程序（完整功能）
├── ccode.go             # 原始ASR程序
├── go.mod               # Go模块定义
├── README.md            # 使用说明
├── cache/              # 缓存目录（自动创建）
└── output_*/           # 处理结果（自动生成）
    ├── audio.mp3
    ├── subtitles.srt
    ├── segments.json
    └── screenshot_*.jpg
```

## 扩展功能

该工具设计为可扩展的，你可以：
- 添加其他ASR服务（如Google Speech-to-Text）
- 集成更多AI模型（如文心一言、通义千问）
- 添加视频编辑功能
- 支持批量处理

## 开发环境

```bash
# 安装依赖
go mod init ccode
go mod tidy

# 编译（可选）
go build -o video-subtitle.exe main_enhanced.go

# 运行
video-subtitle.exe -mode server -port 8080
```

---

**使用愉快！** 🎉

如有问题或建议，欢迎反馈。
