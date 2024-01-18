package handlers

import (
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/hoshinonyaruko/gensokyo-discord/callapi"
	"github.com/hoshinonyaruko/gensokyo-discord/config"
	"github.com/hoshinonyaruko/gensokyo-discord/echo"
	"github.com/hoshinonyaruko/gensokyo-discord/idmap"
	"github.com/hoshinonyaruko/gensokyo-discord/mylog"
)

func init() {
	callapi.RegisterHandler("send_group_msg", HandleSendGroupMsg)
}

func HandleSendGroupMsg(client callapi.Client, s *discordgo.Session, message callapi.ActionMessage) (string, error) {
	// 使用 message.Echo 作为key来获取消息类型
	var msgType string
	if echoStr, ok := message.Echo.(string); ok {
		// 当 message.Echo 是字符串类型时执行此块
		msgType = echo.GetMsgTypeByKey(echoStr)
	}
	if msgType == "" {
		msgType = GetMessageTypeByGroupid(config.GetAppIDStr(), message.Params.GroupID)
	}
	if msgType == "" {
		msgType = GetMessageTypeByUserid(config.GetAppIDStr(), message.Params.UserID)
	}
	if msgType == "" {
		msgType = GetMessageTypeByGroupidV2(message.Params.GroupID)
	}
	if msgType == "" {
		msgType = GetMessageTypeByUseridV2(message.Params.UserID)
	}

	mylog.Printf("send_group_msg获取到信息类型:%v", msgType)
	var idInt64 int64
	var err error
	var retmsg string

	if message.Params.UserID != "" {
		idInt64, err = ConvertToInt64(message.Params.UserID)
	} else if message.Params.GroupID != "" {
		idInt64, err = ConvertToInt64(message.Params.GroupID)
	}

	//设置递归 对直接向gsk发送action时有效果
	if msgType == "" {
		messageCopy := message
		if err != nil {
			mylog.Printf("错误：无法转换 ID %v\n", err)
		} else {
			// 递归3次
			echo.AddMapping(idInt64, 4)
			// 递归调用handleSendGroupMsg，使用设置的消息类型
			echo.AddMsgType(config.GetAppIDStr(), idInt64, "group_private")
			retmsg, _ = HandleSendGroupMsg(client, s, messageCopy)
		}
	}

	switch msgType {
	case "guild":
		//用GroupID给ChannelID赋值,因为我们是把频道虚拟成了群
		message.Params.ChannelID = message.Params.GroupID.(string)
		var RChannelID string
		if message.Params.UserID != nil && config.GetIdmapPro() {
			RChannelID, _, err = idmap.RetrieveRowByIDv2Pro(message.Params.ChannelID, message.Params.UserID.(string))
			mylog.Printf("测试,通过Proid获取的RChannelID:%v", RChannelID)
		}
		if RChannelID == "" {
			// 使用RetrieveRowByIDv2还原真实的ChannelID
			RChannelID, err = idmap.RetrieveRowByIDv2(message.Params.ChannelID)
		}
		if err != nil {
			mylog.Printf("error retrieving real RChannelID: %v", err)
		}
		message.Params.ChannelID = RChannelID
		//这一句是group_private的逻辑,发频道信息用的是channelid
		//message.Params.GroupID = value
		retmsg, _ = HandleSendGuildChannelMsg(client, s, message)
	case "guild_private":
		//用group_id还原出channelid 这是虚拟成群的私聊信息
		var RChannelID string
		var Vuserid string
		message.Params.ChannelID = message.Params.GroupID.(string)
		Vuserid, ok := message.Params.UserID.(string)
		if !ok {
			mylog.Printf("Error illegal UserID")
			return "", nil
		}
		if Vuserid != "" && config.GetIdmapPro() {
			RChannelID, _, err = idmap.RetrieveRowByIDv2Pro(message.Params.ChannelID, Vuserid)
			mylog.Printf("测试,通过Proid获取的RChannelID:%v", RChannelID)
		} else {
			// 使用RetrieveRowByIDv2还原真实的ChannelID
			RChannelID, err = idmap.RetrieveRowByIDv2(message.Params.ChannelID)
		}
		if err != nil {
			mylog.Printf("error retrieving real ChannelID: %v", err)
		}
		//读取ini 通过ChannelID取回之前储存的guild_id
		value, err := idmap.ReadConfigv2(RChannelID, "guild_id")
		if err != nil {
			mylog.Printf("Error reading config: %v", err)
			return "", nil
		}
		retmsg, _ = HandleSendGuildChannelPrivateMsg(client, s, message, &value, &RChannelID)

	default:
		mylog.Printf("Unknown message type: %s", msgType)
	}
	//重置递归类型
	if echo.GetMapping(idInt64) <= 0 {
		echo.AddMsgType(config.GetAppIDStr(), idInt64, "")
	}
	echo.AddMapping(idInt64, echo.GetMapping(idInt64)-1)

	//递归3次枚举类型
	if echo.GetMapping(idInt64) > 0 {
		tryMessageTypes := []string{"group", "guild", "guild_private"}
		messageCopy := message // 创建message的副本
		echo.AddMsgType(config.GetAppIDStr(), idInt64, tryMessageTypes[echo.GetMapping(idInt64)-1])
		delay := config.GetSendDelay()
		time.Sleep(time.Duration(delay) * time.Millisecond)
		HandleSendGroupMsg(client, s, messageCopy)
	}
	return retmsg, nil
}
