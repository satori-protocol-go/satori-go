package define

// 分页列表
type Paginated[T any] struct {
	Data []T    `json:"data"`           // 数据
	Next string `json:"next,omitempty"` // 下一页的令牌
}

// 双向分页列表
type BidiPaginated[T any] struct {
	Data []T    `json:"data"`           // 数据
	Prev string `json:"prev,omitempty"` // 上一页的令牌
	Next string `json:"next,omitempty"` // 下一页的令牌
}
