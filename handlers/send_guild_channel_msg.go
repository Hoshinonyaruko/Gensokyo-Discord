package handlers

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"

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
		replyMsg, err := GenerateReplyMessage(foundItems, messageText)
		if err != nil {
			mylog.Printf("生成消息失败: %v", err)
			return "", err
		}
		mylog.Printf("频道发信息channelID:%v  replyMsg:%v", channelID, replyMsg)
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
func GenerateReplyMessage(foundItems map[string][]string, messageText string) (*discordgo.MessageSend, error) {
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
	// 处理Base64编码的markdown
	if markdowns, ok := foundItems["markdown"]; ok {
		for _, markdown := range markdowns {
			// Base64解码
			data, err := base64.StdEncoding.DecodeString(markdown)
			if err != nil {
				log.Printf("Base64解码失败：%v", err)
				continue
			}

			// 打印解码后的数据（用于调试）
			fmt.Print(string(data))

			// 调用 ConvertToDiscordMessage 生成消息对象
			discordMsg, err := ConvertToDiscordMessage(data)
			if err != nil {
				log.Printf("转换为 Discord 消息失败: %v", err)
				continue
			}

			// 合并返回的消息到当前消息中
			msg.Content += "\n" + discordMsg.Content
			msg.Components = append(msg.Components, discordMsg.Components...)
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

// 定义 JSON 结构的解析对象
type Button struct {
	Action     ActionData `json:"action"`
	RenderData RenderData `json:"render_data"`
}

type ActionData struct {
	Data       string     `json:"data"`
	Enter      bool       `json:"enter"`
	Permission Permission `json:"permission"`
}

type RenderData struct {
	Label        string `json:"label"`
	Style        int    `json:"style"`
	VisitedLabel string `json:"visited_label"`
}

type Permission struct {
	Type int `json:"type"`
}

type Row struct {
	Buttons []Button `json:"buttons"`
}

type KeyboardContent struct {
	Rows []Row `json:"rows"`
}

type Keyboard struct {
	Content KeyboardContent `json:"content"`
}

type Markdown struct {
	Content string `json:"content"`
}

type MessageData struct {
	Keyboard Keyboard `json:"keyboard"`
	Markdown Markdown `json:"markdown"`
}

// 将输入的字节类型 JSON 转换为 Discord 可发送的消息结构
func ConvertToDiscordMessage(jsonData []byte) (*discordgo.MessageSend, error) {
	// 解析 JSON 数据
	var messageData MessageData
	err := json.Unmarshal(jsonData, &messageData)
	if err != nil {
		return nil, fmt.Errorf("JSON 解码失败: %v", err)
	}

	// 创建消息对象
	msg := &discordgo.MessageSend{}

	// 处理 markdown 内容
	if markdownContent := messageData.Markdown.Content; markdownContent != "" {
		msg.Content = ConvertQQBotToMarkdown(markdownContent) // 转换并设置为消息内容
	}

	// 创建按钮组件
	var components []discordgo.MessageComponent
	for _, row := range messageData.Keyboard.Content.Rows {
		var actionRow discordgo.ActionsRow
		for _, button := range row.Buttons {
			var btn discordgo.Button
			if button.Action.Data == "https://gskllm.com" {
				// 如果是外部链接，使用 LinkButton 类型
				btn = discordgo.Button{
					Label: button.RenderData.Label,
					Style: discordgo.LinkButton,                // 设置为 LinkButton 类型
					URL:   button.Action.Data,                  // URL 放在这里
					Emoji: discordgo.ComponentEmoji{Name: "🌙"}, // 添加月亮 emoji
				}
			} else {
				// 普通按钮，触发逻辑操作
				btn = discordgo.Button{
					Label:    button.RenderData.Label,
					Style:    discordgo.PrimaryButton,
					CustomID: button.Action.Data,                  // 使用 CustomID 处理逻辑
					Emoji:    discordgo.ComponentEmoji{Name: "🌙"}, // 添加月亮 emoji
				}
			}

			// 输出调试信息
			fmt.Printf("按钮 Label: %s, CustomID/URL: %s, Style: %d\n", btn.Label, btn.CustomID, btn.Style)

			actionRow.Components = append(actionRow.Components, btn)
		}
		components = append(components, actionRow)
	}

	// 将按钮组件添加到消息中
	msg.Components = components

	return msg, nil
}

// 转换 qqbot 标签为 Discord 支持的 Markdown 格式
func ConvertQQBotToMarkdown(input string) string {
	// 替换 <qqbot-cmd-input> 标签为 Markdown 加粗，只提取 text 部分
	reCmdInput := regexp.MustCompile(`<qqbot-cmd-input text='([^']+)' show='[^']+' reference='[^']+' />`)
	output := reCmdInput.ReplaceAllString(input, "**$1**") // 加粗文本

	// 替换 <qqbot-at-user> 标签为 Discord 提及格式
	reAtUser := regexp.MustCompile(`<qqbot-at-user id="(\d+)" />`)
	output = reAtUser.ReplaceAllString(output, "<@$1>") // 转换为 Discord 提及格式

	return output
}
