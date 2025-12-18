package models

// DataSegment 识别结果数据段
type DataSegment struct {
    Text      string  `json:"text"`
    StartTime float64 `json:"start_time"`
    EndTime   float64 `json:"end_time"`
}
