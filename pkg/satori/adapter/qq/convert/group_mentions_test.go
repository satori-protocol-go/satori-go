package convert

import (
	"strings"
	"testing"

	"github.com/WindowsSov8forUs/botgo-plus/dto"
)

func TestGroupMessageContentMentions(t *testing.T) {
	tests := []struct {
		name    string
		message *dto.Message
		atEvent bool
		want    string
	}{
		{
			name:    "group at event adds self mention",
			message: &dto.Message{Content: "  hello"},
			atEvent: true,
			want:    `<at id="bot-123"/>hello`,
		},
		{
			name: "existing mention maps is_you to login id",
			message: &dto.Message{
				Content:  `hello <@opaque> and <@other>`,
				Mentions: []*dto.User{{ID: "opaque", Username: "Bot", IsYou: true}, {ID: "other", Username: "Other"}},
			},
			want: `hello <at id="bot-123" name="Bot"/> and <at id="other" name="Other"/>`,
		},
		{
			name: "metadata without content mentions adds both targets",
			message: &dto.Message{
				Content:  "hello",
				Mentions: []*dto.User{{ID: "other", Username: "Other"}, {ID: "opaque", Username: "Bot", IsYou: true}, {ID: "everyone", Scope: "all"}},
			},
			want: `<at id="bot-123" name="Bot"/>hello<at id="other" name="Other"/>`,
		},
		{
			name: "QQ native mention token maps is_you",
			message: &dto.Message{
				Content:  `<qqbot-at-user id="opaque" />`,
				Mentions: []*dto.User{{ID: "opaque", IsYou: true}},
			},
			want: `<at id="bot-123"/>`,
		},
		{
			name: "unmatched metadata does not add extra mention",
			message: &dto.Message{
				Content:  `<@other>`,
				Mentions: []*dto.User{{ID: "opaque", IsYou: true}},
			},
			want: `<at id="other"/>`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GroupMessageContent(tt.message, "bot-123", tt.atEvent); got != tt.want {
				t.Fatalf("content = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGroupMentionNameEscapesMarkup(t *testing.T) {
	got := GroupMessageContent(&dto.Message{
		Content:  `<@opaque>`,
		Mentions: []*dto.User{{ID: "opaque", Username: `Bot"><img src="evil"`, IsYou: true}},
	}, "bot-123", false)
	if strings.Contains(got, `<img`) || !strings.Contains(got, `name="Bot&quot;&gt;&lt;img src=&quot;evil&quot;"`) {
		t.Fatalf("unsafe mention name in %q", got)
	}
}
