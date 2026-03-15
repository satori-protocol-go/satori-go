package messagesend

import (
	"strings"

	"github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/messagecodec"
)

type seqCounter struct {
	value int
}

func newSeqCounter(referrer Referrer) *seqCounter {
	if referrer.HasMsgSeq {
		return &seqCounter{value: referrer.MsgSeq}
	}
	return &seqCounter{value: -1}
}

func (s *seqCounter) Current() int {
	return s.value
}

func (s *seqCounter) Next() int {
	s.value++
	return s.value
}

func parseSegments(content string, platform string) []messagecodec.Segment {
	segments, err := messagecodec.Parse(content, platform)
	if err != nil {
		return []messagecodec.Segment{{Text: content}}
	}
	if len(segments) == 0 && strings.TrimSpace(content) != "" {
		return []messagecodec.Segment{{Text: content}}
	}
	return segments
}
