package handlers

import (
	"log"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/hoshinonyaruko/gensokyo-discord/callapi"
	"github.com/hoshinonyaruko/gensokyo-discord/idmap"
	"github.com/hoshinonyaruko/gensokyo-discord/mylog"
)

func init() {
	callapi.RegisterHandler("get_group_ban", SetGroupBan)
}

func SetGroupBan(client callapi.Client, s *discordgo.Session, message callapi.ActionMessage) (string, error) {

	// 从message中获取group_id和UserID
	groupID := message.Params.GroupID.(string)
	receivedUserID := message.Params.UserID.(string)
	//读取ini 通过ChannelID取回之前储存的guild_id
	guildID, err := idmap.ReadConfigv2(groupID, "guild_id")
	if err != nil {
		mylog.Printf("Error reading config: %v", err)
		return "", nil
	}
	// 根据UserID读取真实的userid
	realUserID, err := idmap.RetrieveRowByIDv2(receivedUserID)
	if err != nil {
		mylog.Printf("Error reading real userID: %v", err)
		return "", nil
	}

	// 读取消息类型
	msgType, err := idmap.ReadConfigv2(groupID, "type")
	if err != nil {
		mylog.Printf("Error reading config for message type: %v", err)
		return "", nil
	}

	// 根据消息类型进行操作
	switch msgType {
	case "group":
		mylog.Printf("setGroupBan(频道): 目前暂未开放该能力")
		return "", nil
	case "private":
		mylog.Printf("setGroupBan(频道): 目前暂未适配私聊虚拟群场景的禁言能力")
		return "", nil
	case "guild":
		// 假设 roleID 是无发言权限的角色ID
		// todo 完善这里
		roleID := "特定的角色ID"

		// 将字符串 duration 转换为整数
		duration := message.Params.Duration
		if err != nil {
			log.Printf("Error converting duration: %v", err)
			return "", nil
		}

		if duration == 0 {
			// 解除禁言
			err = s.GuildMemberRoleRemove(guildID, realUserID, roleID)
			if err != nil {
				log.Printf("Error removing mute role from member: %v", err)
			}
		} else {
			// 禁言用户
			err = s.GuildMemberRoleAdd(guildID, realUserID, roleID)
			if err != nil {
				log.Printf("Error adding mute role to member: %v", err)
				return "", nil
			}

			// 设置一个定时器，在指定的时间后解除禁言
			time.AfterFunc(time.Duration(duration)*time.Second, func() {
				err := s.GuildMemberRoleRemove(guildID, realUserID, roleID)
				if err != nil {
					log.Printf("Error automatically unmuting member: %v", err)
				}
			})
		}
	}

	return "", nil
}
