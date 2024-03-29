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
	callapi.RegisterHandler("send_private_msg", HandleSendPrivateMsg)
}

func HandleSendPrivateMsg(client callapi.Client, s *discordgo.Session, message callapi.ActionMessage) (string, error) {
	// 使用 message.Echo 作为key来获取消息类型
	var msgType string
	var retmsg string
	if echoStr, ok := message.Echo.(string); ok {
		// 当 message.Echo 是字符串类型时执行此块
		msgType = echo.GetMsgTypeByKey(echoStr)
	}

	if msgType == "" {
		msgType = GetMessageTypeByUserid(config.GetAppIDStr(), message.Params.UserID)
	}
	if msgType == "" {
		msgType = GetMessageTypeByGroupid(config.GetAppIDStr(), message.Params.GroupID)
	}
	if msgType == "" {
		msgType = GetMessageTypeByUseridV2(message.Params.UserID)
	}
	if msgType == "" {
		msgType = GetMessageTypeByGroupidV2(message.Params.GroupID)
	}
	var idInt64 int64
	var err error

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
			// 递归调用handleSendPrivateMsg，使用设置的消息类型
			echo.AddMsgType(config.GetAppIDStr(), idInt64, "group_private")
			HandleSendPrivateMsg(client, s, messageCopy)
		}
	}

	switch msgType {

	case "guild_private":
		//当收到发私信调用 并且来源是频道
		retmsg, _ = HandleSendGuildChannelPrivateMsg(client, s, message, nil, nil)
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
		HandleSendPrivateMsg(client, s, messageCopy)
	}
	return retmsg, nil
}

// 处理频道私信 最后2个指针参数可空 代表使用userid倒推
func HandleSendGuildChannelPrivateMsg(client callapi.Client, s *discordgo.Session, message callapi.ActionMessage, optionalGuildID *string, optionalChannelID *string) (string, error) {
	params := message.Params
	messageText, foundItems := parseMessageContent(params)

	var err error
	var UserID string
	var GroupID string
	var retmsg string
	if message.Params.GroupID != nil {
		if gid, ok := message.Params.GroupID.(string); ok {
			GroupID = gid // GroupID 是 string 类型
		} else {
			mylog.Printf(" GroupID 不是 string,304")
		}
	} else {
		mylog.Printf("GroupID 为 nil,信息发送正常可忽略")
	}
	RawUserID := message.Params.UserID.(string)

	// 使用 echo 获取消息ID
	var messageID string
	if config.GetLazyMessageId() {
		//由于实现了Params的自定义unmarshell 所以可以类型安全的断言为string
		messageID = echo.GetLazyMessagesId(RawUserID)
		mylog.Printf("GetLazyMessagesId: %v", messageID)
	}
	if messageID == "" {
		if echoStr, ok := message.Echo.(string); ok {
			messageID = echo.GetMsgIDByKey(echoStr)
			mylog.Println("echo取私聊发信息对应的message_id:", messageID)
		}
	}
	mylog.Println("私聊信息messageText:", messageText)
	//获取guild和channelid和message id流程 没有RawUserID情况下
	if RawUserID != "" {
		//频道私信 转 私信
		if GroupID != "" && config.GetIdmapPro() {
			_, UserID, err = idmap.RetrieveRowByIDv2Pro(GroupID, RawUserID)
			if err != nil {
				mylog.Printf("Error reading config: %v", err)
				return "", nil
			}
			mylog.Printf("测试,通过Proid获取的UserID:%v", UserID)
		} else {
			UserID, err = idmap.RetrieveRowByIDv2(RawUserID)
			if err != nil {
				mylog.Printf("Error reading config: %v", err)
				return "", nil
			}
		}
		// 如果messageID为空，通过函数获取
		if messageID == "" {
			messageID = GetMessageIDByUseridOrGroupid(config.GetAppIDStr(), UserID)
			mylog.Println("通过GetMessageIDByUserid函数获取的message_id:", messageID)
		}

	} else {
		RawUserID, err := idmap.ReadConfigv2(GroupID, "user_id")
		if err != nil {
			mylog.Printf("send_private_msg,获取user_id出错:%v", err)
		}
		//频道私信 转 群聊 获取userid
		if GroupID != "" && config.GetIdmapPro() {
			_, UserID, err = idmap.RetrieveRowByIDv2Pro(GroupID, RawUserID)
			if err != nil {
				mylog.Printf("Error reading config: %v", err)
				return "", nil
			}
			mylog.Printf("测试,通过Proid获取的UserID:%v", UserID)
		} else {
			UserID, err = idmap.RetrieveRowByIDv2(RawUserID)
			if err != nil {
				mylog.Printf("Error reading config1: %v", err)
				return "", nil
			}
		}
		mylog.Println("群组(私信虚拟成的)发信息messageText:", messageText)
		// 如果messageID为空，通过函数获取
		if messageID == "" {
			messageID = GetMessageIDByUseridOrGroupid(config.GetAppIDStr(), UserID)
			mylog.Println("通过GetMessageIDByUserid函数获取的message_id:", messageID)
		}
	}

	//开发环境用
	if config.GetDevMsgID() {
		messageID = "1000"
	}
	mylog.Printf("创建私信频道: %v", UserID)
	// 创建一个与用户的私信频道
	dmChannel, err := s.UserChannelCreate(UserID)
	if err != nil {
		mylog.Printf("创建私信频道失败: %v", err)
		return "", nil
	}

	// 使用GenerateReplyMessage函数处理所有类型的消息
	combinedMsg, err := GenerateReplyMessage(foundItems, messageText)
	if err != nil {
		mylog.Printf("生成消息失败: %v", err)
		return "", nil
	}

	// 向私信频道发送消息
	_, err = s.ChannelMessageSendComplex(dmChannel.ID, combinedMsg)
	if err != nil {
		mylog.Printf("发送私信失败: %v", err)
		return "", nil
	}

	// 发送回执
	retmsg, _ = SendResponse(client, err, &message)
	return retmsg, nil
}
