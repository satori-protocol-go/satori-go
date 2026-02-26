package operation

import (
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/event"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
)

// OpCode 信令类型
type Opcode uint8

const (
	OpcodeEvent    Opcode = iota // 接收 事件
	OpcodePing                   // 发送 心跳
	OpcodePong                   // 接收 心跳回复
	OpcodeIdentify               // 发送 鉴权
	OpcodeReady                  // 接收 鉴权成功
	OpcodeMeta                   // 接收 元信息更新
)

// Operation WebSocket 发送的信令的数据结构
type Operation struct {
	Op   Opcode `json:"op"`             // 信令类型
	Body any    `json:"body,omitempty"` // 信令数据
}

// EVENT 信令的 Body 数据
type EventBody event.Event

// IDENTIFY 信令的 Body 数据
type IdentifyBody struct {
	Token string `json:"token,omitempty"` // 鉴权令牌
	Sn    int64  `json:"sn,omitempty"`    // 序列号
}

// READY 信令的 Body 数据
type ReadyBody struct {
	Logins    []*login.Login `json:"logins"`     // 登录信息
	ProxyUrls []string       `json:"proxy_urls"` // 代理路由 列表
}

// META 信令的 Body 数据
type MetaBody struct {
	ProxyUrls []string `json:"proxy_urls"` // 代理路由 列表
}
