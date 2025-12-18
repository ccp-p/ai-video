# 音频/视频AI总结工具

一个基于Go的高性能音频视频处理工具，支持语音识别(ASR)、字幕生成和AI智能总结。

## 🎯 核心功能

- **视频处理**：自动提取音频、生成SRT字幕、截图
- **语音识别**：使用B站必剪API进行高精度语音转文字
- **AI总结**：本地算法生成结构化Markdown总结
- **前后端分离**：纯HTML+Vue前端，易于部署和修改
- **文件管理**：自动读取D:/download目录，无需手动输入路径

## 📁 项目结构

```
ccode/
├── main.go                 # 后端主程序
├── static/
│   └── index.html         # 前端界面
├── cache/                 # ASR缓存目录
├── go.mod                 # Go依赖配置
└── README.md              # 说明文档
```

## 🚀 快速开始

### 环境要求

1. **Go 1.18+**
2. **FFmpeg** (必须安装并添加到PATH)
   - 下载：https://ffmpeg.org/download.html
   - 验证：`ffmpeg -version`

### 安装依赖

```bash
go mod tidy
```

### 启动服务

**HTTP模式 (推荐):**
```bash
go run main.go -mode server -port 8080
```

**CLI模式:**
```bash
# 处理视频
go run main.go -mode cli -video D:/download/demo.mp4

# 处理音频
go run main.go -mode cli -audio D:/download/audio.mp3
```

### 使用Web界面

1. 启动服务后，浏览器访问：`http://localhost:8080`
2. 界面会自动读取 `D:/download` 目录
3. 点击文件选择，然后操作

## 🎨 前端特性

### UI优化重点
- **AI总结为核心**：将总结结果放在最显眼位置
- **简化操作**：左侧控制面板，右侧结果展示
- **清晰分类**：三个标签页分别展示不同内容
  - **AI总结**：核心要点 + 完整Markdown
  - **原始数据**：视频信息、识别段落、SRT字幕
  - **截图管理**：截图路径和说明

### 交互优化
- ✅ 无需手动输入文件路径（自动读取D:/download）
- ✅ 一键处理和一键总结
- ✅ 进度条实时显示
- ✅ 错误信息清晰提示
- ✅ 自动保存配置
- ✅ 纯HTML，无需构建工具

## 🔧 API接口

### 文件列表
```bash
GET /api/list-files
# 返回D:/download目录下的文件列表
```

### 视频处理
```bash
POST /api/process-video
Content-Type: application/json

{
  "video_path": "D:/download/video.mp4"
}

# 返回：音频路径、字幕、截图、识别结果等
```

### AI总结
```bash
POST /api/ai-summarize
Content-Type: application/json

{
  "text": "要总结的文本内容...",
  "prompt": "自定义提示词（可选）",
  "screenshots": ["screenshot_1.jpg"]
}

# 返回：总结内容、Markdown、要点列表
```

### 配置API
```bash
POST /api/config
Content-Type: application/json

{
  "api_key": "...",
  "api_url": "...",
  "model": "gpt-4",
  "custom_prompt": "自定义总结要求..."
}
```

## 📝 使用流程

1. **准备视频**
   - 将视频文件放入 `D:/download` 目录
   - 确保路径包含英文，避免中文路径问题

2. **处理视频**
   - 在左侧"选择文件"点击视频
   - 点击"开始处理视频"
   - 等待进度条完成

3. **生成总结**
   - 处理完成后，文本自动填入左侧
   - 可选：填入截图路径（自动填充）
   - 点击"生成AI总结"按钮

4. **查看结果**
   - 切换到"AI总结"标签页
   - 查看核心要点和完整总结
   - 复制Markdown内容用于其他用途

## 🔧 配置说明

### 默认配置（本地算法）
- 无需API Key
- 使用Go内置算法提取关键句
- 生成Markdown格式输出

### 配置外部AI API
- 在"AI配置"面板填入信息
- 支持OpenAI、文心一言等API
- 系统会自动优先使用外部API

## 📊 输出说明

处理完成后会在视频同目录创建 `output_视频名` 文件夹：
- `audio.mp3` - 提取的音频
- `subtitles.srt` - SRT字幕文件
- `segments.json` - 识别结果JSON
- `screenshot_*.jpg` - 视频截图（5张）

## ⚠️ 注意事项

1. **FFmpeg必须安装**
   - 用于音频提取和截图
   - 建议下载完整版（包含ffprobe）

2. **目录权限**
   - 确保程序有读写D:/download的权限
   - 可以在其他盘创建符号链接

3. **网络要求**
   - 需要访问B站API进行语音识别
   - 识别过程可能需要几分钟

4. **缓存机制**
   - 相同音频会自动使用缓存
   - 提高重复处理速度
   - 缓存文件在 `./cache` 目录

## 🔍 故障排除

### 问题：找不到ffmpeg
```
错误：未找到ffmpeg，请确保已安装并添加到PATH
解决：下载FFmpeg并配置环境变量
```

### 问题：目录不存在
```
错误：下载目录不存在: D:/download
解决：手动创建目录：mkdir D:/download
```

### 问题：识别失败
```
可能原因：
- 网络问题，无法访问B站API
- 音频格式不支持
- 音频文件太大
解决：检查网络，尝试重新上传
```

## 🛠️ 开发说明

### 前端修改
- 文件位置：`static/index.html`
- 直接编辑即可，无需构建
- 使用Vue 3 CDN版本

### 后端修改
- 文件位置：`main.go`
- 修改常量 `DOWNLOAD_DIR` 可更改默认目录
- 修改 `HTTP_PORT` 更改端口

### 添加新功能
1. 在 `main.go` 添加新的API路由
2. 在前端 `static/index.html` 添加对应UI
3. 重启服务即可生效

## 📞 技术支持

本工具使用以下技术栈：
- **后端**: Go (标准库 + net/http)
- **前端**: Vue 3 + 原生HTML/CSS
- **语音识别**: B站必剪API
- **视频处理**: FFmpeg

---

**版本**: 2.0 (前后端分离版)
**更新日期**: 2025-12-18
