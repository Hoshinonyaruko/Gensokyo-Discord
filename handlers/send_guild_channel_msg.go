package handlers

import (
	"bytes"
	"encoding/base64"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/bwmarrin/discordgo"
	"github.com/hoshinonyaruko/gensokyo-discord/callapi"
	"github.com/hoshinonyaruko/gensokyo-discord/config"
	"github.com/hoshinonyaruko/gensokyo-discord/idmap"
	"github.com/hoshinonyaruko/gensokyo-discord/mylog"

	"github.com/hoshinonyaruko/gensokyo-discord/echo"
)

func init() {
	callapi.RegisterHandler("send_guild_channel_msg", HandleSendGuildChannelMsg)
}

func HandleSendGuildChannelMsg(client callapi.Client, s *discordgo.Session, message callapi.ActionMessage) (string, error) {
	// 使用 message.Echo 作为key来获取消息类型
	var msgType string
	var retmsg string
	if echoStr, ok := message.Echo.(string); ok {
		// 当 message.Echo 是字符串类型时执行此块
		msgType = echo.GetMsgTypeByKey(echoStr)
	}
	//如果获取不到 就用group_id获取信息类型
	if msgType == "" {
		msgType = GetMessageTypeByGroupid(config.GetAppIDStr(), message.Params.GroupID)
	}
	//如果获取不到 就用user_id获取信息类型
	if msgType == "" {
		msgType = GetMessageTypeByUserid(config.GetAppIDStr(), message.Params.UserID)
	}
	//新增 内存获取不到从数据库获取
	if msgType == "" {
		msgType = GetMessageTypeByUseridV2(message.Params.UserID)
	}
	if msgType == "" {
		msgType = GetMessageTypeByGroupidV2(message.Params.GroupID)
	}
	//当不转换频道信息时(不支持频道私聊)
	if msgType == "" {
		msgType = "guild"
	}
	switch msgType {
	//原生guild信息
	case "guild":
		params := message.Params
		messageText, foundItems := parseMessageContent(params)

		channelID := params.ChannelID
		// 使用 echo 获取消息ID
		var messageID string
		if config.GetLazyMessageId() {
			//由于实现了Params的自定义unmarshell 所以可以类型安全的断言为string
			messageID = echo.GetLazyMessagesId(channelID)
			mylog.Printf("GetLazyMessagesId: %v", messageID)
		}
		if messageID == "" {
			if echoStr, ok := message.Echo.(string); ok {
				messageID = echo.GetMsgIDByKey(echoStr)
				mylog.Println("echo取频道发信息对应的message_id:", messageID)
			}
		}
		if messageID == "" {
			messageID = GetMessageIDByUseridOrGroupid(config.GetAppIDStr(), channelID)
			mylog.Println("通过GetMessageIDByUseridOrGroupid函数获取的message_id:", messageID)
		}
		//开发环境用
		if config.GetDevMsgID() {
			messageID = "1000"
		}
		mylog.Println("频道发信息messageText:", messageText)
		//mylog.Println("foundItems:", foundItems)
		// 优先发送文本信息
		var err error
		// 统一处理发送逻辑
		msgseq := echo.GetMappingSeq(messageID)
		echo.AddMappingSeq(messageID, msgseq+1)

		// 使用GenerateReplyMessage函数处理所有类型的消息
		replyMsg, err := GenerateReplyMessage(messageID, foundItems, messageText)
		if err != nil {
			mylog.Printf("生成消息失败: %v", err)
			return "", err
		}

		_, err = s.ChannelMessageSendComplex(channelID, replyMsg)
		if err != nil {
			mylog.Printf("发送消息失败: %v", err)
			return "", err
		}

		// 发送回执
		retmsg, _ := SendResponse(client, err, &message)
		return retmsg, nil
	//频道私信 此时直接取出
	case "guild_private":
		params := message.Params
		channelID := params.ChannelID
		guildID := params.GuildID
		var RChannelID string
		var err error
		// 使用RetrieveRowByIDv2还原真实的ChannelID
		RChannelID, err = idmap.RetrieveRowByIDv2(channelID)
		if err != nil {
			mylog.Printf("error retrieving real UserID: %v", err)
		}
		retmsg, _ = HandleSendGuildChannelPrivateMsg(client, s, message, &guildID, &RChannelID)
	default:
		mylog.Printf("2Unknown message type: %s", msgType)
	}
	return retmsg, nil
}

// GenerateReplyMessage 创建一个discordgo兼容的消息，支持多个图片
func GenerateReplyMessage(messageID string, foundItems map[string][]string, messageText string) (*discordgo.MessageSend, error) {
	msg := &discordgo.MessageSend{
		Content: messageText,
	}

	// 处理本地图片
	if imageURLs, ok := foundItems["local_image"]; ok {
		for _, imageURL := range imageURLs {
			fileData, err := os.ReadFile(imageURL)
			if err != nil {
				log.Printf("无法读取图片文件：%v", err)
				continue
			}

			msg.Files = append(msg.Files, &discordgo.File{
				Name:   "image.png",
				Reader: bytes.NewReader(fileData),
			})
		}
	}

	// 处理网络图片
	if imageURLs, ok := foundItems["url_image"]; ok {
		for _, imageURL := range imageURLs {
			if config.GetUrlPicTransfer() {
				// 如果配置为true，下载并转换为Base64
				base64Data, err := downloadImage("http://" + imageURL)
				if err != nil {
					// 处理错误
					continue
				}

				msg.Files = append(msg.Files, &discordgo.File{
					Name:   "image.png",
					Reader: bytes.NewReader(base64Data),
				})
			} else {
				// 否则直接使用URL
				msg.Embeds = append(msg.Embeds, &discordgo.MessageEmbed{
					Image: &discordgo.MessageEmbedImage{
						URL: "http://" + imageURL,
					},
				})
			}
		}
	}

	// 处理网络图片
	if imageURLs, ok := foundItems["url_images"]; ok {
		for _, imageURL := range imageURLs {
			if config.GetUrlPicTransfer() {
				// 如果配置为true，下载并转换为Base64
				base64Data, err := downloadImage("https://" + imageURL)
				if err != nil {
					// 处理错误
					continue
				}

				msg.Files = append(msg.Files, &discordgo.File{
					Name:   "image.png",
					Reader: bytes.NewReader(base64Data),
				})
			} else {
				// 否则直接使用URL
				msg.Embeds = append(msg.Embeds, &discordgo.MessageEmbed{
					Image: &discordgo.MessageEmbedImage{
						URL: "https://" + imageURL,
					},
				})
			}
		}
	}

	// 处理Base64编码的图片
	if base64URLs, ok := foundItems["base64_image"]; ok {
		for _, base64URL := range base64URLs {
			data, err := base64.StdEncoding.DecodeString(base64URL)
			if err != nil {
				log.Printf("Base64解码失败：%v", err)
				continue
			}

			msg.Files = append(msg.Files, &discordgo.File{
				Name:   "image.png",
				Reader: bytes.NewReader(data),
			})
		}
	}

	return msg, nil
}

// 下载图片
func downloadImage(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return []byte(data), nil
}
