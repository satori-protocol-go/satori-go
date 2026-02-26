package guild

// 群组
type Guild struct {
	Id     string `json:"id"`               // 群组 ID
	Name   string `json:"name,omitempty"`   // 群组名称
	Avatar string `json:"avatar,omitempty"` // 群组头像
}
