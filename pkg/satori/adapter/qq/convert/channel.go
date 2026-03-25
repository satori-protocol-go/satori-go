package convert

import (
	"strings"

	"github.com/WindowsSov8forUs/botgo-plus/dto"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model"
	"github.com/satori-protocol-go/satori-go/pkg/satori/model/channel"
)

func ChannelFromDTO(input *dto.Channel) *channel.Channel {
	if input == nil {
		return nil
	}
	return &channel.Channel{
		Id:       input.ID,
		Type:     channelTypeFromDTO(input.Type),
		Name:     input.Name,
		ParentId: input.ParentID,
	}
}

func ParseChannelValue(raw model.Channel) *dto.ChannelValueObject {
	value := &dto.ChannelValueObject{}
	value.Name = strings.TrimSpace(raw.Name)
	value.ParentID = strings.TrimSpace(raw.ParentId)
	switch raw.Type {
	case channel.ChannelTypeVoice:
		value.Type = dto.ChannelTypeVoice
	case channel.ChannelTypeCategory:
		value.Type = dto.ChannelTypeCategory
	default:
		value.Type = dto.ChannelTypeText
	}
	return value
}

func channelTypeFromDTO(raw dto.ChannelType) channel.ChannelType {
	switch raw {
	case dto.ChannelTypeVoice:
		return channel.ChannelTypeVoice
	case dto.ChannelTypeCategory:
		return channel.ChannelTypeCategory
	default:
		return channel.ChannelTypeText
	}
}
