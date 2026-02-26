package element

import "github.com/satori-protocol-go/satori-go/pkg/satori/internal/attr"

// 资源元素
//
// 资源消息元素表示文本中存在的资源文件。不同的平台对资源文件的支持存在较大的差异。
// 发送时只需提供 src。
// 如果某个平台不支持特定的资源类型，适配器应该用 src 代替。
// 如果某个平台不支持将资源消息元素和其他消息元素同时发送，适配器应该分多条发送。
type Resource struct {
	Src     string `attr:"src"`               // 收发 资源的 URL
	Title   string `attr:"title,omitempty"`   // 收发 资源文件名称
	Cache   bool   `attr:"cache,omitempty"`   // 发 是否使用已缓存的文件
	Timeout int    `attr:"timeout,omitempty"` // 发 下载文件的最长时间 (毫秒)
}

// <img> 元素用于表示图片。
type Img struct {
	BaseElement
	Resource
	Width  int `attr:"width,omitempty"`  // 收 图片宽度（像素）
	Height int `attr:"height,omitempty"` // 收 图片高度（像素）
}

func (i *Img) Tag() string {
	return "img"
}

func (i *Img) Alias() []string {
	return []string{"image"}
}

func (i *Img) UnmarshalAttrs(attrs map[string]any) error {
	err := attr.UnmarshalAttrs(i, attrs)
	if err != nil {
		return err
	}
	return i.BaseElement.UnmarshalAttrs(attrs)
}

// <audio> 元素用于表示语音。
type Audio struct {
	BaseElement
	NoAlias
	Resource
	Duration float64 `attr:"duration,omitempty"` // 收发 音频长度 (秒)
	Poster   string  `attr:"poster,omitempty"`   // 收发 音频封面 URL
}

func (a *Audio) Tag() string {
	return "audio"
}

func (a *Audio) UnmarshalAttrs(attrs map[string]any) error {
	err := attr.UnmarshalAttrs(a, attrs)
	if err != nil {
		return err
	}
	return a.BaseElement.UnmarshalAttrs(attrs)
}

// <video> 元素用于表示视频。
type Video struct {
	BaseElement
	NoAlias
	Resource
	Width    int     `attr:"width,omitempty"`    // 收 视频宽度（像素）
	Height   int     `attr:"height,omitempty"`   // 收 视频高度（像素）
	Duration float64 `attr:"duration,omitempty"` // 收 视频长度 (秒)
	Poster   string  `attr:"poster,omitempty"`   // 收发 视频封面 URL
}

func (v *Video) Tag() string {
	return "video"
}

func (v *Video) UnmarshalAttrs(attrs map[string]any) error {
	err := attr.UnmarshalAttrs(v, attrs)
	if err != nil {
		return err
	}
	return v.BaseElement.UnmarshalAttrs(attrs)
}

// <file> 元素用于表示文件。
type File struct {
	BaseElement
	NoAlias
	Resource
	Poster string `attr:"poster,omitempty"` // 收发 缩略图 URL
}

func (f *File) Tag() string {
	return "file"
}

func (f *File) UnmarshalAttrs(attrs map[string]any) error {
	err := attr.UnmarshalAttrs(f, attrs)
	if err != nil {
		return err
	}
	return f.BaseElement.UnmarshalAttrs(attrs)
}
