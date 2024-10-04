package handlers

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/hoshinonyaruko/gensokyo-discord/callapi"
	"github.com/hoshinonyaruko/gensokyo-discord/config"
	"github.com/hoshinonyaruko/gensokyo-discord/echo"
	"github.com/hoshinonyaruko/gensokyo-discord/idmap"
	"github.com/hoshinonyaruko/gensokyo-discord/mylog"
)

var BotID string
var AppID string

// 定义响应结构体
type ServerResponse struct {
	Data struct {
		MessageID int `json:"message_id"`
	} `json:"data"`
	Message string      `json:"message"`
	RetCode int         `json:"retcode"`
	Status  string      `json:"status"`
	Echo    interface{} `json:"echo"`
}

// 发送成功回执 todo 返回可互转的messageid
func SendResponse(client callapi.Client, err error, message *callapi.ActionMessage) (string, error) {
	// 设置响应值
	response := ServerResponse{}
	response.Data.MessageID = 0 // todo 实现messageid转换
	response.Echo = message.Echo
	if err != nil {
		response.Message = err.Error() // 可选：在响应中添加错误消息
		//response.RetCode = -1          // 可以是任何非零值，表示出错
		//response.Status = "failed"
		response.RetCode = 0 //官方api审核异步的 审核中默认返回失败,但其实信息发送成功了
		response.Status = "ok"
	} else {
		response.Message = ""
		response.RetCode = 0
		response.Status = "ok"
	}

	// 转化为map并发送
	outputMap := structToMap(response)
	// 将map转换为JSON字符串
	jsonResponse, jsonErr := json.Marshal(outputMap)
	if jsonErr != nil {
		log.Printf("Error marshaling response to JSON: %v", jsonErr)
		return "", jsonErr
	}
	//发送给ws 客户端
	sendErr := client.SendMessage(outputMap)
	if sendErr != nil {
		mylog.Printf("Error sending message via client: %v", sendErr)
		return "", sendErr
	}

	mylog.Printf("发送成功回执: %+v", string(jsonResponse))
	return string(jsonResponse), nil
}

// 信息处理函数
func parseMessageContent(paramsMessage callapi.ParamsContent) (string, map[string][]string) {
	messageText := ""

	foundItems := make(map[string][]string)

	switch message := paramsMessage.Message.(type) {
	case string:
		mylog.Printf("params.message is a string\n")
		messageText = message

	case []interface{}:
		mylog.Printf("params.message is a slice (segment_type_koishi)\n")
		for _, segment := range message {
			segmentMap, ok := segment.(map[string]interface{})
			if !ok {
				continue
			}

			segmentType, ok := segmentMap["type"].(string)
			if !ok {
				continue
			}

			segmentContent := ""
			switch segmentType {
			case "text":
				segmentContent, _ = segmentMap["data"].(map[string]interface{})["text"].(string)

			case "image":
				fileContent, _ := segmentMap["data"].(map[string]interface{})["file"].(string)
				foundItems["image"] = append(foundItems["image"], fileContent)

			case "voice", "record":
				fileContent, _ := segmentMap["data"].(map[string]interface{})["file"].(string)
				foundItems["record"] = append(foundItems["record"], fileContent)

			case "at":
				qqNumber, _ := segmentMap["data"].(map[string]interface{})["qq"].(string)
				foundItems["at"] = append(foundItems["at"], qqNumber)

			case "markdown":
				mdContent, ok := segmentMap["data"].(map[string]interface{})["data"]
				if ok {
					var mdContentEncoded string
					if mdContentMap, isMap := mdContent.(map[string]interface{}); isMap {
						mdContentBytes, err := json.Marshal(mdContentMap)
						if err != nil {
							mylog.Printf("Error marshaling mdContentMap to JSON:%v", err)
							continue
						}
						mdContentEncoded = base64.StdEncoding.EncodeToString(mdContentBytes)
					} else if mdContentStr, isString := mdContent.(string); isString {
						if strings.HasPrefix(mdContentStr, "base64://") {
							mdContentEncoded = mdContentStr
						} else {
							mdContentStr = strings.ReplaceAll(mdContentStr, "&amp;", "&")
							mdContentStr = strings.ReplaceAll(mdContentStr, "&#91;", "[")
							mdContentStr = strings.ReplaceAll(mdContentStr, "&#93;", "]")
							mdContentStr = strings.ReplaceAll(mdContentStr, "&#44;", ",")

							var jsonMap map[string]interface{}
							if err := json.Unmarshal([]byte(mdContentStr), &jsonMap); err != nil {
								mylog.Printf("Error unmarshaling string to JSON:%v", err)
								continue
							}
							mdContentBytes, err := json.Marshal(jsonMap)
							if err != nil {
								mylog.Printf("Error marshaling jsonMap to JSON:%v", err)
								continue
							}
							mdContentEncoded = base64.StdEncoding.EncodeToString(mdContentBytes)
						}
					} else {
						mylog.Printf("Error marshaling markdown segment wrong type.")
						continue
					}
					foundItems["markdown"] = append(foundItems["markdown"], mdContentEncoded)
				} else {
					mylog.Printf("Error: markdown segment data is nil.")
				}

			default:
				mylog.Printf("Unhandled segment type: %s", segmentType)
			}

			messageText += segmentContent

		}
	case map[string]interface{}:
		mylog.Printf("params.message is a map (segment_type_trss)\n")
		messageType, _ := message["type"].(string)

		switch messageType {
		case "text":
			messageText, _ = message["data"].(map[string]interface{})["text"].(string)

		case "image":
			fileContent, _ := message["data"].(map[string]interface{})["file"].(string)
			foundItems["image"] = append(foundItems["image"], fileContent)

		case "voice", "record":
			fileContent, _ := message["data"].(map[string]interface{})["file"].(string)
			foundItems["record"] = append(foundItems["record"], fileContent)

		case "at":
			qqNumber, _ := message["data"].(map[string]interface{})["qq"].(string)
			foundItems["at"] = append(foundItems["at"], qqNumber)

		case "markdown":
			mdContent, ok := message["data"].(map[string]interface{})["data"]
			if ok {
				var mdContentEncoded string
				if mdContentMap, isMap := mdContent.(map[string]interface{}); isMap {
					mdContentBytes, err := json.Marshal(mdContentMap)
					if err != nil {
						mylog.Printf("Error marshaling mdContentMap to JSON:%v", err)
					}
					mdContentEncoded = base64.StdEncoding.EncodeToString(mdContentBytes)
				} else if mdContentStr, isString := mdContent.(string); isString {
					if strings.HasPrefix(mdContentStr, "base64://") {
						mdContentEncoded = mdContentStr
					} else {
						mdContentStr = strings.ReplaceAll(mdContentStr, "&amp;", "&")
						mdContentStr = strings.ReplaceAll(mdContentStr, "&#91;", "[")
						mdContentStr = strings.ReplaceAll(mdContentStr, "&#93;", "]")
						mdContentStr = strings.ReplaceAll(mdContentStr, "&#44;", ",")
						var jsonMap map[string]interface{}
						if err := json.Unmarshal([]byte(mdContentStr), &jsonMap); err != nil {
							mylog.Printf("Error unmarshaling string to JSON:%v", err)
						}
						mdContentBytes, err := json.Marshal(jsonMap)
						if err != nil {
							mylog.Printf("Error marshaling jsonMap to JSON:%v", err)
						}
						mdContentEncoded = base64.StdEncoding.EncodeToString(mdContentBytes)
					}
				} else {
					mylog.Printf("Error: markdown content has an unexpected type.")
				}
				foundItems["markdown"] = append(foundItems["markdown"], mdContentEncoded)
			} else {
				mylog.Printf("Error: markdown segment data is nil.")
			}

		default:
			mylog.Printf("Unhandled message type: %s", messageType)
		}

	default:
		mylog.Println("Unsupported message format: params.message field is not a string, map or slice")
	}

	// 当匹配到复古cq码上报类型,使用低效率正则.
	if _, ok := paramsMessage.Message.(string); ok {
		// 正则表达式部分
		var localImagePattern *regexp.Regexp
		var localRecordPattern *regexp.Regexp
		if runtime.GOOS == "windows" {
			localImagePattern = regexp.MustCompile(`\[CQ:image,file=file:///([^\]]+?)\]`)
		} else {
			localImagePattern = regexp.MustCompile(`\[CQ:image,file=file://([^\]]+?)\]`)
		}
		if runtime.GOOS == "windows" {
			localRecordPattern = regexp.MustCompile(`\[CQ:record,file=file:///([^\]]+?)\]`)
		} else {
			localRecordPattern = regexp.MustCompile(`\[CQ:record,file=file://([^\]]+?)\]`)
		}
		httpUrlImagePattern := regexp.MustCompile(`\[CQ:image,file=http://(.+?)\]`)
		httpsUrlImagePattern := regexp.MustCompile(`\[CQ:image,file=https://(.+?)\]`)
		base64ImagePattern := regexp.MustCompile(`\[CQ:image,file=base64://(.+?)\]`)
		base64RecordPattern := regexp.MustCompile(`\[CQ:record,file=base64://(.+?)\]`)
		httpUrlRecordPattern := regexp.MustCompile(`\[CQ:record,file=http://(.+?)\]`)
		httpsUrlRecordPattern := regexp.MustCompile(`\[CQ:record,file=https://(.+?)\]`)
		httpUrlVideoPattern := regexp.MustCompile(`\[CQ:video,file=http://(.+?)\]`)
		httpsUrlVideoPattern := regexp.MustCompile(`\[CQ:video,file=https://(.+?)\]`)
		mdPattern := regexp.MustCompile(`\[CQ:markdown,data=base64://(.+?)\]`)
		qqMusicPattern := regexp.MustCompile(`\[CQ:music,type=qq,id=(\d+)\]`)

		patterns := []struct {
			key     string
			pattern *regexp.Regexp
		}{
			{"local_image", localImagePattern},
			{"url_image", httpUrlImagePattern},
			{"url_images", httpsUrlImagePattern},
			{"base64_image", base64ImagePattern},
			{"base64_record", base64RecordPattern},
			{"local_record", localRecordPattern},
			{"url_record", httpUrlRecordPattern},
			{"url_records", httpsUrlRecordPattern},
			{"markdown", mdPattern},
			{"qqmusic", qqMusicPattern},
			{"url_video", httpUrlVideoPattern},
			{"url_videos", httpsUrlVideoPattern},
		}

		for _, pattern := range patterns {
			matches := pattern.pattern.FindAllStringSubmatch(messageText, -1)
			for _, match := range matches {
				if len(match) > 1 {
					foundItems[pattern.key] = append(foundItems[pattern.key], match[1])
				}
			}
			// 移动替换操作到这里，确保所有匹配都被处理后再进行替换
			messageText = pattern.pattern.ReplaceAllString(messageText, "")
		}
	}

	// for key, items := range foundItems {
	// 	fmt.Printf("Key: %s, Items: %v\n", key, items)
	// }
	return messageText, foundItems
}

// func isIPAddress(address string) bool {
// 	return net.ParseIP(address) != nil
// }

// 处理at和其他定形文到onebotv11格式(cq码)
func RevertTransformedText(data interface{}, msgtype string, s *discordgo.Session, vgid int64) string {
	var msg *discordgo.Message
	var menumsg bool
	var messageText string
	switch v := data.(type) {
	case *discordgo.MessageCreate:
		msg = v.Message
	// case *dto.WSATMessageData:
	// 	msg = (*dto.Message)(v)
	// case *dto.WSMessageData:
	// 	msg = (*dto.Message)(v)
	// case *dto.WSDirectMessageData:
	// 	msg = (*dto.Message)(v)
	// case *dto.WSC2CMessageData:
	// 	msg = (*dto.Message)(v)
	default:
		return ""
	}
	menumsg = false
	//单独一个空格的信息的空格用户并不希望去掉
	if msg.Content == " " {
		menumsg = true
		messageText = " "
	}

	if !menumsg {
		//处理前 先去前后空
		messageText = strings.TrimSpace(msg.Content)
	}

	// 将messageText里的BotID替换成AppID
	messageText = strings.ReplaceAll(messageText, BotID, AppID)

	// 使用正则表达式来查找所有<@!数字>的模式
	re := regexp.MustCompile(`<@!(\d+)>`)
	// 使用正则表达式来替换找到的模式为[CQ:at,qq=用户ID]
	messageText = re.ReplaceAllStringFunc(messageText, func(m string) string {
		submatches := re.FindStringSubmatch(m)
		if len(submatches) > 1 {
			userID := submatches[1]
			// 检查是否是 BotID，如果是则直接返回，不进行映射,或根据用户需求移除
			if userID == AppID {
				if config.GetRemoveAt() {
					return ""
				} else {
					return "[CQ:at,qq=" + AppID + "]"
				}
			}

			// 不是 BotID，进行正常映射
			userID64, err := idmap.StoreIDv2(userID)
			if err != nil {
				//如果储存失败(数据库损坏)返回原始值
				mylog.Printf("Error storing ID: %v", err)
				return "[CQ:at,qq=" + userID + "]"
			}
			// 类型转换
			userIDStr := strconv.FormatInt(userID64, 10)
			// 经过转换的cq码
			return "[CQ:at,qq=" + userIDStr + "]"
		}
		return m
	})
	//结构 <@!>空格/内容
	//如果移除了前部at,信息就会以空格开头,因为只移去了最前面的at,但at后紧跟随一个空格
	if config.GetRemoveAt() {
		if !menumsg {
			//再次去前后空
			messageText = strings.TrimSpace(messageText)
		}
	}

	// 检查是否需要移除前缀
	if config.GetRemovePrefixValue() {
		// 移除消息内容中第一次出现的 "/"
		if idx := strings.Index(messageText, "/"); idx != -1 {
			messageText = messageText[:idx] + messageText[idx+1:]
		}
	}

	// 检查是否启用白名单模式
	if config.GetWhitePrefixMode() {
		// 获取白名单例外群数组（现在返回 int64 数组）
		whiteBypass := config.GetWhiteBypass()
		bypass := false

		// 检查vgid是否在白名单例外数组中
		for _, id := range whiteBypass {
			if id == vgid {
				bypass = true
				break
			}
		}

		// 如果vgid不在白名单例外数组中，则应用白名单过滤
		if !bypass {
			// 获取白名单数组
			whitePrefixes := config.GetWhitePrefixs()
			// 加锁以安全地读取 TemporaryCommands
			idmap.MutexT.Lock()
			temporaryCommands := make([]string, len(idmap.TemporaryCommands))
			copy(temporaryCommands, idmap.TemporaryCommands)
			idmap.MutexT.Unlock()

			// 合并白名单和临时指令
			allPrefixes := append(whitePrefixes, temporaryCommands...)
			// 默认设置为不匹配
			matched := false

			// 遍历白名单数组，检查是否有匹配项
			for _, prefix := range allPrefixes {
				if strings.HasPrefix(messageText, prefix) {
					// 找到匹配项，保留 messageText 并跳出循环
					matched = true
					break
				}
			}

			// 如果没有匹配项，则将 messageText 置为兜底回复 兜底回复可空
			if !matched {
				messageText = ""
				SendMessage(config.GetNoWhiteResponse(), data, msgtype, s)
			}
		}
	}

	//检查是否启用黑名单模式
	if config.GetBlackPrefixMode() {
		// 获取黑名单数组
		blackPrefixes := config.GetBlackPrefixs()
		// 遍历黑名单数组，检查是否有匹配项
		for _, prefix := range blackPrefixes {
			if strings.HasPrefix(messageText, prefix) {
				// 找到匹配项，将 messageText 置为空并停止处理
				messageText = ""
				break
			}
		}
	}
	//移除以GetVisualkPrefixs数组开头的文本
	visualkPrefixs := config.GetVisualkPrefixs()
	for _, prefix := range visualkPrefixs {
		if strings.HasPrefix(messageText, prefix) {
			// 检查 messageText 是否比 prefix 长，这意味着后面还有其他内容
			if len(messageText) > len(prefix) {
				// 移除找到的前缀
				messageText = strings.TrimPrefix(messageText, prefix)
			}
			break // 只移除第一个匹配的前缀
		}
	}
	// 处理图片附件
	for _, attachment := range msg.Attachments {
		if strings.HasPrefix(attachment.ContentType, "image/") {
			// 获取文件的后缀名
			ext := filepath.Ext(attachment.Filename)
			md5name := strings.TrimSuffix(attachment.Filename, ext)

			// 检查 URL 是否已包含协议头
			var url string
			if strings.HasPrefix(attachment.URL, "http://") || strings.HasPrefix(attachment.URL, "https://") {
				url = attachment.URL
			} else {
				url = "http://" + attachment.URL // 默认使用 http，也可以根据需要改为 https
			}

			imageCQ := "[CQ:image,file=" + md5name + ".image,subType=0,url=" + url + "]"
			messageText += imageCQ
		}
	}

	return messageText
}

// 将收到的data.content转换为message segment todo,群场景不支持受图片,频道场景的图片可以拼一下
func ConvertToSegmentedMessage(data interface{}) []map[string]interface{} {
	// 强制类型转换，获取Message结构
	var msg *discordgo.Message
	var menumsg bool
	switch v := data.(type) {
	case *discordgo.MessageCreate:
		msg = v.Message
	default:
		return nil
	}
	menumsg = false
	//单独一个空格的信息的空格用户并不希望去掉
	if msg.Content == " " {
		menumsg = true
	}
	var messageSegments []map[string]interface{}

	// 处理Attachments字段来构建图片消息
	for _, attachment := range msg.Attachments {
		imageFileMD5 := attachment.Filename
		for _, ext := range []string{"{", "}", ".png", ".jpg", ".gif", "-"} {
			imageFileMD5 = strings.ReplaceAll(imageFileMD5, ext, "")
		}
		imageSegment := map[string]interface{}{
			"type": "image",
			"data": map[string]interface{}{
				"file":    imageFileMD5 + ".image",
				"subType": "0",
				"url":     attachment.URL,
			},
		}
		messageSegments = append(messageSegments, imageSegment)

		// 在msg.Content中替换旧的图片链接
		//newImagePattern := "[CQ:image,file=" + attachment.URL + "]"
		//msg.Content = msg.Content + newImagePattern
	}
	// 将msg.Content里的BotID替换成AppID
	msg.Content = strings.ReplaceAll(msg.Content, BotID, AppID)
	// 使用正则表达式查找所有的[@数字]格式
	r := regexp.MustCompile(`<@!(\d+)>`)
	atMatches := r.FindAllStringSubmatch(msg.Content, -1)
	for _, match := range atMatches {
		userID := match[1]

		if userID == AppID {
			if config.GetRemoveAt() {
				// 根据配置移除
				msg.Content = strings.Replace(msg.Content, match[0], "", 1)
				continue // 跳过当前循环迭代
			} else {
				//将其转换为AppID
				userID = AppID
				// 构建at部分的映射并加入到messageSegments
				atSegment := map[string]interface{}{
					"type": "at",
					"data": map[string]interface{}{
						"qq": userID,
					},
				}
				messageSegments = append(messageSegments, atSegment)
				// 从原始内容中移除at部分
				msg.Content = strings.Replace(msg.Content, match[0], "", 1)
				continue // 跳过当前循环迭代
			}
		}
		// 不是 AppID，进行正常处理
		userID64, err := idmap.StoreIDv2(userID)
		if err != nil {
			// 如果存储失败，记录错误并继续使用原始 userID
			mylog.Printf("Error storing ID: %v", err)
		} else {
			// 类型转换成功，使用新的 userID
			userID = strconv.FormatInt(userID64, 10)
		}

		// 构建at部分的映射并加入到messageSegments
		atSegment := map[string]interface{}{
			"type": "at",
			"data": map[string]interface{}{
				"qq": userID,
			},
		}
		messageSegments = append(messageSegments, atSegment)

		// 从原始内容中移除at部分
		msg.Content = strings.Replace(msg.Content, match[0], "", 1)
	}
	//结构 <@!>空格/内容
	//如果移除了前部at,信息就会以空格开头,因为只移去了最前面的at,但at后紧跟随一个空格
	if config.GetRemoveAt() {
		//再次去前后空
		if !menumsg {
			msg.Content = strings.TrimSpace(msg.Content)
		}
	}

	// 检查是否需要移除前缀
	if config.GetRemovePrefixValue() {
		// 移除消息内容中第一次出现的 "/"
		if idx := strings.Index(msg.Content, "/"); idx != -1 {
			msg.Content = msg.Content[:idx] + msg.Content[idx+1:]
		}
	}
	// 如果还有其他内容，那么这些内容被视为文本部分
	if msg.Content != "" {
		textSegment := map[string]interface{}{
			"type": "text",
			"data": map[string]interface{}{
				"text": msg.Content,
			},
		}
		messageSegments = append(messageSegments, textSegment)
	}
	//排列
	messageSegments = sortMessageSegments(messageSegments)
	return messageSegments
}

// ConvertToInt64 尝试将 interface{} 类型的值转换为 int64 类型
func ConvertToInt64(value interface{}) (int64, error) {
	switch v := value.(type) {
	case int:
		return int64(v), nil
	case int64:
		return v, nil
	case string:
		return strconv.ParseInt(v, 10, 64)
	default:
		// 当无法处理该类型时返回错误
		return 0, fmt.Errorf("无法将类型 %T 转换为 int64", value)
	}
}

// 排列MessageSegments
func sortMessageSegments(segments []map[string]interface{}) []map[string]interface{} {
	var atSegments, textSegments, imageSegments []map[string]interface{}

	for _, segment := range segments {
		switch segment["type"] {
		case "at":
			atSegments = append(atSegments, segment)
		case "text":
			textSegments = append(textSegments, segment)
		case "image":
			imageSegments = append(imageSegments, segment)
		}
	}

	// 按照指定的顺序合并这些切片
	return append(append(atSegments, textSegments...), imageSegments...)
}

// SendMessage 发送消息根据不同的类型
func SendMessage(messageText string, data interface{}, messageType string, s *discordgo.Session) error {
	// 强制类型转换，获取Message结构
	var msg *discordgo.Message
	switch v := data.(type) {
	case *discordgo.MessageCreate:
		msg = v.Message
	default:
		return nil
	}
	switch messageType {
	case "guild":
		// 处理公会消息
		msgseq := echo.GetMappingSeq(msg.ID)
		echo.AddMappingSeq(msg.ID, msgseq+1)

		// 创建foundItems
		foundItems := make(map[string][]string)

		combinedMsg, err := GenerateReplyMessage(foundItems, messageText)
		if err != nil {
			mylog.Printf("生成消息失败: %v", err)
			return err
		}

		if _, err := s.ChannelMessageSendComplex(msg.ChannelID, combinedMsg); err != nil {
			mylog.Printf("发送消息失败: %v", err)
			return err
		}

	case "guild_private":
		// 处理私信
		msgseq := echo.GetMappingSeq(msg.ID)
		echo.AddMappingSeq(msg.ID, msgseq+1)

		// 创建foundItems映射并添加文本消息
		foundItems := make(map[string][]string)

		combinedMsg, err := GenerateReplyMessage(foundItems, messageText)
		if err != nil {
			mylog.Printf("生成消息失败: %v", err)
			return err
		}

		// 获取用户ID
		userID := msg.Author.ID

		// 创建一个与用户的私信频道
		dmChannel, err := s.UserChannelCreate(userID)
		if err != nil {
			mylog.Printf("创建私信频道失败: %v", err)
			return err
		}

		// 向私信频道发送消息
		if _, err := s.ChannelMessageSendComplex(dmChannel.ID, combinedMsg); err != nil {
			mylog.Printf("发送私信失败: %v", err)
			return err
		}

	default:
		return errors.New("未知的消息类型")
	}

	return nil
}

// 将map转化为json string
func ConvertMapToJSONString(m map[string]interface{}) (string, error) {
	// 使用 json.Marshal 将 map 转换为 JSON 字节切片
	jsonBytes, err := json.Marshal(m)
	if err != nil {
		log.Printf("Error marshalling map to JSON: %v", err)
		return "", err
	}

	// 将字节切片转换为字符串
	jsonString := string(jsonBytes)
	return jsonString, nil
}

// 将结构体转换为 map[string]interface{}
func structToMap(obj interface{}) map[string]interface{} {
	out := make(map[string]interface{})
	j, _ := json.Marshal(obj)
	json.Unmarshal(j, &out)
	return out
}

// 通过user_id获取类型
func GetMessageTypeByUserid(appID string, userID interface{}) string {
	// 从appID和userID生成key
	var userIDStr string
	switch u := userID.(type) {
	case int:
		userIDStr = strconv.Itoa(u)
	case int64:
		userIDStr = strconv.FormatInt(u, 10)
	case float64:
		userIDStr = strconv.FormatFloat(u, 'f', 0, 64)
	case string:
		userIDStr = u
	default:
		// 可能需要处理其他类型或报错
		return ""
	}

	key := appID + "_" + userIDStr
	return echo.GetMsgTypeByKey(key)
}

// 通过user_id获取类型
func GetMessageTypeByUseridV2(userID interface{}) string {
	// 从appID和userID生成key
	var userIDStr string
	switch u := userID.(type) {
	case int:
		userIDStr = strconv.Itoa(u)
	case int64:
		userIDStr = strconv.FormatInt(u, 10)
	case float64:
		userIDStr = strconv.FormatFloat(u, 'f', 0, 64)
	case string:
		userIDStr = u
	default:
		// 可能需要处理其他类型或报错
		return ""
	}
	msgtype, _ := idmap.ReadConfigv2(userIDStr, "type")
	// if err != nil {
	// 	//mylog.Printf("GetMessageTypeByUseridV2失败:%v", err)
	// }
	return msgtype
}

// 通过group_id获取类型
func GetMessageTypeByGroupid(appID string, GroupID interface{}) string {
	// 从appID和userID生成key
	var GroupIDStr string
	switch u := GroupID.(type) {
	case int:
		GroupIDStr = strconv.Itoa(u)
	case int64:
		GroupIDStr = strconv.FormatInt(u, 10)
	case string:
		GroupIDStr = u
	default:
		// 可能需要处理其他类型或报错
		return ""
	}

	key := appID + "_" + GroupIDStr
	return echo.GetMsgTypeByKey(key)
}

// 通过group_id获取类型
func GetMessageTypeByGroupidV2(GroupID interface{}) string {
	// 从appID和userID生成key
	var GroupIDStr string
	switch u := GroupID.(type) {
	case int:
		GroupIDStr = strconv.Itoa(u)
	case int64:
		GroupIDStr = strconv.FormatInt(u, 10)
	case string:
		GroupIDStr = u
	default:
		// 可能需要处理其他类型或报错
		return ""
	}

	msgtype, _ := idmap.ReadConfigv2(GroupIDStr, "type")
	// if err != nil {
	// 	//mylog.Printf("GetMessageTypeByGroupidV2失败:%v", err)
	// }
	return msgtype
}
