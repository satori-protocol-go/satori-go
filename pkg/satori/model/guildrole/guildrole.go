package guildrole

// 群组角色
type GuildRole struct {
	Id   string `json:"id"`             // 角色 ID
	Name string `json:"name,omitempty"` // 角色名称
}
