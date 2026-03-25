package convert

import (
	"fmt"
	"strings"

	"github.com/satori-protocol-go/satori-go/pkg/satori/internal/xhtml"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/message/element"
)

type MessageResourceKind string

const (
	MessageResourceImage MessageResourceKind = "image"
	MessageResourceAudio MessageResourceKind = "audio"
	MessageResourceVideo MessageResourceKind = "video"
	MessageResourceFile  MessageResourceKind = "file"
)

type MessageResource struct {
	Kind MessageResourceKind
	Src  string
}

type MessageSegment struct {
	Text     string
	QuoteID  string
	Resource *MessageResource
	ArkJSON  string
	Markdown bool
	Buttons  [][]MessageButton
}

type MessageButton struct {
	Id    string
	Type  string
	Href  string
	Text  string
	Theme string
	Label string
}

func ParseMessageSegments(content string, platform string) []MessageSegment {
	segments, err := parseQQMessageSegments(content, platform)
	if err != nil {
		return []MessageSegment{{Text: content}}
	}
	if len(segments) == 0 && strings.TrimSpace(content) != "" {
		return []MessageSegment{{Text: content}}
	}
	return segments
}

type qqMessageParser struct {
	platform     string
	pendingQuote string
	currentText  strings.Builder
	segments     []MessageSegment
}

func parseQQMessageSegments(content string, platform string) ([]MessageSegment, error) {
	rawElements := xhtml.Parse(content, nil)
	elements, err := element.Transform(rawElements)
	if err != nil {
		return []MessageSegment{{Text: content}}, err
	}

	state := &qqMessageParser{
		platform: platform,
		segments: make([]MessageSegment, 0, len(elements)),
	}
	state.walk(elements)
	state.flushText()

	return state.segments, nil
}

func (p *qqMessageParser) walk(elements []element.Element) {
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
			if typed.Id != "" {
				p.pendingQuote = typed.Id
			}
		case *element.At:
			p.writeText(p.renderAt(typed))
		case *element.Sharp:
			p.writeText(p.renderSharp(typed))
		case *element.A:
			if typed.Href != "" {
				p.writeText(typed.Href)
			}
			p.walk(typed.Children())
		case *element.Img:
			p.appendResource(MessageResourceImage, typed.Src)
		case *element.Audio:
			p.appendResource(MessageResourceAudio, typed.Src)
		case *element.Video:
			p.appendResource(MessageResourceVideo, typed.Src)
		case *element.File:
			p.appendResource(MessageResourceFile, typed.Src)
		case *element.Extension:
			if p.tryAppendExtendedSegment(typed) {
				continue
			}
			p.writeText(p.renderExtension(typed))
			p.walk(typed.Children())
		case *element.Button:
			// Buttons are only meaningful in qq markdown mode.
			if p.platform == "qq" {
				p.flushText()
				row := []MessageButton{parseMessageButton(typed)}
				p.segments = append(p.segments, MessageSegment{
					QuoteID:  p.consumeQuote(),
					Markdown: true,
					Buttons:  [][]MessageButton{row},
				})
				continue
			}
			p.walk(typed.Children())
		default:
			p.walk(typed.Children())
		}
	}
}

func (p *qqMessageParser) writeText(text string) {
	if text == "" {
		return
	}
	p.currentText.WriteString(text)
}

func (p *qqMessageParser) flushText() {
	text := p.currentText.String()
	if text == "" {
		return
	}
	p.segments = append(p.segments, MessageSegment{Text: text, QuoteID: p.consumeQuote()})
	p.currentText.Reset()
}

func (p *qqMessageParser) appendResource(kind MessageResourceKind, src string) {
	if src == "" {
		return
	}
	p.flushText()
	p.segments = append(p.segments, MessageSegment{QuoteID: p.consumeQuote(), Resource: &MessageResource{Kind: kind, Src: src}})
}

func (p *qqMessageParser) consumeQuote() string {
	value := p.pendingQuote
	p.pendingQuote = ""
	return value
}

func (p *qqMessageParser) renderAt(input *element.At) string {
	if input == nil {
		return ""
	}
	if input.Type == "all" {
		if p.platform == "qqguild" {
			return "<qqbot-at-everyone />"
		}
		return "@everyone"
	}
	if input.Id == "" {
		return ""
	}
	if p.platform == "qqguild" {
		return fmt.Sprintf(`<qqbot-at-user id="%s" />`, input.Id)
	}
	return fmt.Sprintf("<@%s>", input.Id)
}

func (p *qqMessageParser) renderSharp(input *element.Sharp) string {
	if input == nil || input.Id == "" {
		return ""
	}
	if p.platform == "qqguild" {
		return fmt.Sprintf("<#!%s>", input.Id)
	}
	return "#" + input.Id
}

func (p *qqMessageParser) renderExtension(input *element.Extension) string {
	if input == nil {
		return ""
	}
	switch input.Tag() {
	case "chronocat:emoji", "qq:emoji":
		if raw, ok := input.Get("id"); ok {
			return fmt.Sprintf("<emoji:%v>", raw)
		}
	}
	return ""
}

func (p *qqMessageParser) tryAppendExtendedSegment(input *element.Extension) bool {
	if input == nil || p.platform != "qq" {
		return false
	}
	switch input.Tag() {
	case "qq:ark":
		p.flushText()
		raw := strings.TrimSpace(flattenElementsText(input.Children()))
		if raw == "" {
			return true
		}
		p.segments = append(p.segments, MessageSegment{
			QuoteID: p.consumeQuote(),
			ArkJSON: raw,
		})
		return true
	case "markdown":
		p.flushText()
		text := strings.TrimSpace(flattenElementsText(input.Children()))
		p.segments = append(p.segments, MessageSegment{
			QuoteID:  p.consumeQuote(),
			Text:     text,
			Markdown: true,
		})
		return true
	case "qq:button-group":
		p.flushText()
		rows := parseButtonRows(input.Children())
		if len(rows) == 0 {
			return true
		}
		p.segments = append(p.segments, MessageSegment{
			QuoteID:  p.consumeQuote(),
			Markdown: true,
			Buttons:  rows,
		})
		return true
	default:
		return false
	}
}

func parseMessageButton(input *element.Button) MessageButton {
	if input == nil {
		return MessageButton{}
	}
	return MessageButton{
		Id:    strings.TrimSpace(input.Id),
		Type:  strings.TrimSpace(input.Type),
		Href:  strings.TrimSpace(input.Href),
		Text:  strings.TrimSpace(input.Text),
		Theme: strings.TrimSpace(input.Theme),
		Label: strings.TrimSpace(flattenElementsText(input.Children())),
	}
}

func parseButtonRows(elements []element.Element) [][]MessageButton {
	rows := [][]MessageButton{}
	current := []MessageButton{}
	push := func() {
		if len(current) == 0 {
			return
		}
		row := make([]MessageButton, len(current))
		copy(row, current)
		rows = append(rows, row)
		current = current[:0]
	}
	for _, item := range elements {
		switch typed := item.(type) {
		case *element.Button:
			current = append(current, parseMessageButton(typed))
			if len(current) >= 5 {
				push()
			}
		case *element.Br:
			push()
		case *element.P:
			push()
			childRows := parseButtonRows(typed.Children())
			rows = append(rows, childRows...)
		default:
			childRows := parseButtonRows(typed.Children())
			if len(childRows) > 0 {
				push()
				rows = append(rows, childRows...)
			}
		}
	}
	push()
	return rows
}

func flattenElementsText(elements []element.Element) string {
	var builder strings.Builder
	for _, item := range elements {
		if item == nil {
			continue
		}
		switch typed := item.(type) {
		case *element.Text:
			builder.WriteString(typed.Text)
		case *element.Br:
			builder.WriteString("\n")
		default:
			builder.WriteString(flattenElementsText(typed.Children()))
		}
	}
	return builder.String()
}
