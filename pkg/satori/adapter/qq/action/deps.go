package action

import (
	"context"

	botgodto "github.com/WindowsSov8forUs/botgo-plus/dto"
	botgoopenapi "github.com/WindowsSov8forUs/botgo-plus/openapi"
	qqmessagesend "github.com/satori-protocol-go/satori-go/pkg/satori/adapter/qq/messagesend"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/login"
	satoriserver "github.com/satori-protocol-go/satori-go/pkg/satori/server"
)

type APIV1 interface {
	Message(ctx context.Context, channelID string, messageID string) (*botgodto.Message, error)
	PostMessage(ctx context.Context, channelID string, msg *botgodto.MessageToCreate) (*botgodto.Message, error)
	PostDirectMessage(
		ctx context.Context,
		dm *botgodto.DirectMessage,
		msg *botgodto.MessageToCreate,
	) (*botgodto.Message, error)
	RetractMessage(
		ctx context.Context,
		channelID string,
		msgID string,
		options ...botgoopenapi.RetractMessageOption,
	) error
	RetractDMMessage(
		ctx context.Context,
		guildID string,
		msgID string,
		options ...botgoopenapi.RetractMessageOption,
	) error
	CreateDirectMessage(
		ctx context.Context,
		dm *botgodto.DirectMessageToCreate,
	) (*botgodto.DirectMessage, error)
}

type APIV2 interface {
	PostGroupMessage(
		ctx context.Context,
		groupID string,
		msg botgodto.APIMessage,
	) (*botgodto.GroupMessageResponse, error)
	PostC2CMessage(
		ctx context.Context,
		userID string,
		msg botgodto.APIMessage,
	) (*botgodto.C2CMessageResponse, error)
	RetractGroupMessage(
		ctx context.Context,
		groupID string,
		msgID string,
		options ...botgoopenapi.RetractMessageOption,
	) error
	RetractC2CMessage(
		ctx context.Context,
		userID string,
		msgID string,
		options ...botgoopenapi.RetractMessageOption,
	) error
}

type Dependencies struct {
	APIV1 APIV1
	APIV2 APIV2

	EnsureLogins func(context.Context) error
	FindLogin    func(platform string, selfID string) *login.Login

	HandleInternal func(
		request satoriserver.Request[map[string]any],
		path string,
	) (*satoriserver.Response, error)

	MessageSender *qqmessagesend.Sender
}
