package messagecodec

import (
	"fmt"
	"strings"

	"github.com/satori-protocol-go/satori-go/pkg/satori/internal/xhtml"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message/element"
)

type ResourceKind string

const (
	ResourceImage ResourceKind = "image"
	ResourceAudio ResourceKind = "audio"
	ResourceVideo ResourceKind = "video"
	ResourceFile  ResourceKind = "file"
)

type Resource struct {
	Kind ResourceKind
	Src  string
}

type Segment struct {
	Text     string
	QuoteID  string
	Resource *Resource
}

type parser struct {
	platform     string
	pendingQuote string
	currentText  strings.Builder
	segments     []Segment
}

func Parse(content string, platform string) ([]Segment, error) {
	rawElements := xhtml.Parse(content, nil)
	elements, err := element.Transform(rawElements)
	if err != nil {
		return []Segment{{Text: content}}, err
	}

	state := &parser{
		platform: platform,
		segments: make([]Segment, 0, len(elements)),
	}
	state.walk(elements)
	state.flushText()

	return state.segments, nil
}

func (p *parser) walk(elements []element.Element) {
	for _, item := range elements {
		if item == nil {
			continue
		}
		switch typed := item.(type) {
		case *element.Text:
			p.writeText(typed.Text)
		case *element.Br:
			p.writeText("\n")
		case *element.P:
			if !strings.HasSuffix(p.currentText.String(), "\n") {
				p.writeText("\n")
			}
			p.walk(typed.Children())
			if !strings.HasSuffix(p.currentText.String(), "\n") {
				p.writeText("\n")
			}
		case *element.Message:
			p.flushText()
			p.walk(typed.Children())
			p.flushText()
		case *element.Quote:
			p.flushText()
			if strings.TrimSpace(typed.Id) != "" {
				p.pendingQuote = strings.TrimSpace(typed.Id)
			}
		case *element.At:
			p.writeText(p.renderAt(typed))
		case *element.Sharp:
			p.writeText(p.renderSharp(typed))
		case *element.A:
			if strings.TrimSpace(typed.Href) != "" {
				p.writeText(typed.Href)
			}
			p.walk(typed.Children())
		case *element.Img:
			p.appendResource(ResourceImage, typed.Src)
		case *element.Audio:
			p.appendResource(ResourceAudio, typed.Src)
		case *element.Video:
			p.appendResource(ResourceVideo, typed.Src)
		case *element.File:
			p.appendResource(ResourceFile, typed.Src)
		case *element.Extension:
			p.writeText(p.renderExtension(typed))
			p.walk(typed.Children())
		default:
			p.walk(typed.Children())
		}
	}
}

func (p *parser) writeText(text string) {
	if text == "" {
		return
	}
	p.currentText.WriteString(text)
}

func (p *parser) flushText() {
	text := p.currentText.String()
	if text == "" {
		return
	}
	p.segments = append(p.segments, Segment{
		Text:    text,
		QuoteID: p.consumeQuote(),
	})
	p.currentText.Reset()
}

func (p *parser) appendResource(kind ResourceKind, src string) {
	src = strings.TrimSpace(src)
	if src == "" {
		return
	}
	p.flushText()
	p.segments = append(p.segments, Segment{
		QuoteID:  p.consumeQuote(),
		Resource: &Resource{Kind: kind, Src: src},
	})
}

func (p *parser) consumeQuote() string {
	value := p.pendingQuote
	p.pendingQuote = ""
	return value
}

func (p *parser) renderAt(input *element.At) string {
	if input == nil {
		return ""
	}
	if input.Type == "all" {
		if p.platform == "qqguild" {
			return "<qqbot-at-everyone />"
		}
		return "@everyone"
	}
	if strings.TrimSpace(input.Id) == "" {
		return ""
	}
	if p.platform == "qqguild" {
		return fmt.Sprintf(`<qqbot-at-user id="%s" />`, input.Id)
	}
	return fmt.Sprintf("<@%s>", input.Id)
}

func (p *parser) renderSharp(input *element.Sharp) string {
	if input == nil || strings.TrimSpace(input.Id) == "" {
		return ""
	}
	if p.platform == "qqguild" {
		return fmt.Sprintf("<#!%s>", input.Id)
	}
	return "#" + input.Id
}

func (p *parser) renderExtension(input *element.Extension) string {
	if input == nil {
		return ""
	}
	tag := input.Tag()
	switch tag {
	case "chronocat:emoji", "qq:emoji":
		if raw, ok := input.Get("id"); ok {
			return fmt.Sprintf("<emoji:%v>", raw)
		}
	}
	return ""
}
