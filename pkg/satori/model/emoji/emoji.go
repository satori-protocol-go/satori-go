package emoji

// 表情
type Emoji struct {
	Id   string `json:"id"`             // 表情 ID
	Name string `json:"name,omitempty"` // 表情名称
}
