// 处理收到的信息事件
package Processor

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/hashicorp/go-multierror"
	"github.com/hoshinonyaruko/gensokyo-discord/callapi"
	"github.com/hoshinonyaruko/gensokyo-discord/config"
	"github.com/hoshinonyaruko/gensokyo-discord/echo"
	"github.com/hoshinonyaruko/gensokyo-discord/handlers"
	"github.com/hoshinonyaruko/gensokyo-discord/idmap"
	"github.com/hoshinonyaruko/gensokyo-discord/mylog"
	"github.com/hoshinonyaruko/gensokyo-discord/wsclient"
)

// Processor 结构体用于处理消息
type Processors struct {
	Settings        *config.Settings                  // 使用指针
	Wsclient        []*wsclient.WebSocketClient       // 指针的切片
	WsServerClients []callapi.WebSocketServerClienter //ws server被连接的客户端
}

type Sender struct {
	Nickname string `json:"nickname"`
	TinyID   string `json:"tiny_id"`
	UserID   int64  `json:"user_id"`
	Role     string `json:"role,omitempty"`
	Card     string `json:"card,omitempty"`
	Sex      string `json:"sex,omitempty"`
	Age      int32  `json:"age,omitempty"`
	Area     string `json:"area,omitempty"`
	Level    string `json:"level,omitempty"`
	Title    string `json:"title,omitempty"`
}

// 频道信息事件
type OnebotChannelMessage struct {
	ChannelID   string      `json:"channel_id"`
	GuildID     string      `json:"guild_id"`
	Message     interface{} `json:"message"`
	MessageID   string      `json:"message_id"`
	MessageType string      `json:"message_type"`
	PostType    string      `json:"post_type"`
	SelfID      int64       `json:"self_id"`
	SelfTinyID  string      `json:"self_tiny_id"`
	Sender      Sender      `json:"sender"`
	SubType     string      `json:"sub_type"`
	Time        int64       `json:"time"`
	Avatar      string      `json:"avatar,omitempty"`
	UserID      int64       `json:"user_id"`
	RawMessage  string      `json:"raw_message"`
	Echo        string      `json:"echo,omitempty"`
}

// 群信息事件
type OnebotGroupMessage struct {
	RawMessage      string      `json:"raw_message"`
	MessageID       int         `json:"message_id"`
	GroupID         int64       `json:"group_id"` // Can be either string or int depending on p.Settings.CompleteFields
	MessageType     string      `json:"message_type"`
	PostType        string      `json:"post_type"`
	SelfID          int64       `json:"self_id"` // Can be either string or int
	Sender          Sender      `json:"sender"`
	SubType         string      `json:"sub_type"`
	Time            int64       `json:"time"`
	Avatar          string      `json:"avatar,omitempty"`
	Echo            string      `json:"echo,omitempty"`
	Message         interface{} `json:"message"` // For array format
	MessageSeq      int         `json:"message_seq"`
	Font            int         `json:"font"`
	UserID          int64       `json:"user_id"`
	RealMessageType string      `json:"real_message_type"`  //当前信息的真实类型 group group_private guild guild_private
	IsBindedGroupId bool        `json:"is_binded_group_id"` //当前群号是否是binded后的
	IsBindedUserId  bool        `json:"is_binded_user_id"`  //当前用户号号是否是binded后的
}

type OnebotGroupMessageS struct {
	RawMessage      string      `json:"raw_message"`
	MessageID       string      `json:"message_id"`
	GroupID         string      `json:"group_id"` // Can be either string or int depending on p.Settings.CompleteFields
	MessageType     string      `json:"message_type"`
	PostType        string      `json:"post_type"`
	SelfID          int64       `json:"self_id"` // Can be either string or int
	Sender          Sender      `json:"sender"`
	SubType         string      `json:"sub_type"`
	Time            int64       `json:"time"`
	Avatar          string      `json:"avatar,omitempty"`
	Echo            string      `json:"echo,omitempty"`
	Message         interface{} `json:"message"` // For array format
	MessageSeq      int         `json:"message_seq"`
	Font            int         `json:"font"`
	UserID          string      `json:"user_id"`
	RealMessageType string      `json:"real_message_type,omitempty"`  //当前信息的真实类型 group group_private guild guild_private
	RealUserID      string      `json:"real_user_id,omitempty"`       //当前真实uid
	RealGroupID     string      `json:"real_group_id,omitempty"`      //当前真实gid
	IsBindedGroupId bool        `json:"is_binded_group_id,omitempty"` //当前群号是否是binded后的
	IsBindedUserId  bool        `json:"is_binded_user_id,omitempty"`  //当前用户号号是否是binded后的
}

// 私聊信息事件
type OnebotPrivateMessage struct {
	RawMessage      string        `json:"raw_message"`
	MessageID       int           `json:"message_id"` // Can be either string or int depending on logic
	MessageType     string        `json:"message_type"`
	PostType        string        `json:"post_type"`
	SelfID          int64         `json:"self_id"` // Can be either string or int depending on logic
	Sender          PrivateSender `json:"sender"`
	SubType         string        `json:"sub_type"`
	Time            int64         `json:"time"`
	Avatar          string        `json:"avatar,omitempty"`
	Echo            string        `json:"echo,omitempty"`
	Message         interface{}   `json:"message"`           // For array format
	MessageSeq      int           `json:"message_seq"`       // Optional field
	Font            int           `json:"font"`              // Optional field
	UserID          int64         `json:"user_id"`           // Can be either string or int depending on logic
	RealMessageType string        `json:"real_message_type"` //当前信息的真实类型 group group_private guild guild_private
	IsBindedUserId  bool          `json:"is_binded_user_id"` //当前用户号号是否是binded后的
}

type PrivateSender struct {
	Nickname string `json:"nickname"`
	UserID   int64  `json:"user_id"` // Can be either string or int depending on logic
}

func FoxTimestamp() int64 {
	return time.Now().Unix()
}

// ProcessInlineSearch 处理内联查询
// func (p *Processors) ProcessInlineSearch(data *dto.WSInteractionData) error {
// 	//ctx := context.Background() // 或从更高级别传递一个上下文

// 	// 在这里处理内联查询
// 	// 这可能涉及解析查询、调用某些API、获取结果并格式化为响应
// 	// ...

// 	// 示例：发送响应
// 	// response := "Received your interaction!"            // 创建响应消息
// 	// err := p.api.PostInteractionResponse(ctx, response) // 替换为您的OpenAPI方法
// 	// if err != nil {
// 	// 	return err
// 	// }

// 	return nil
// }

//return nil

//下面是测试时候固定代码
//发私信给机器人4条机器人不回,就不能继续发了

// timestamp := time.Now().Unix() // 获取当前时间的int64类型的Unix时间戳
// timestampStr := fmt.Sprintf("%d", timestamp)

// dm := &dto.DirectMessage{
// 	GuildID:    GuildID,
// 	ChannelID:  ChannelID,
// 	CreateTime: timestampStr,
// }

// PrintStructWithFieldNames(dm)

// // 发送默认回复
// toCreate := &dto.MessageToCreate{
// 	Content: "默认私信回复",
// 	MsgID:   data.ID,
// }
// _, err = p.Api.PostDirectMessage(
// 	context.Background(), dm, toCreate,
// )
// if err != nil {
// 	mylog.Println("Error sending default reply:", err)
// 	return nil
// }

// 打印结构体的函数
func PrintStructWithFieldNames(v interface{}) {
	val := reflect.ValueOf(v)

	// 如果是指针，获取其指向的元素
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	typ := val.Type()

	// 确保我们传入的是一个结构体
	if typ.Kind() != reflect.Struct {
		mylog.Println("Input is not a struct")
		return
	}

	// 迭代所有的字段并打印字段名和值
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		value := val.Field(i)
		mylog.Printf("%s: %v\n", field.Name, value.Interface())
	}
}

// 将结构体转换为 map[string]interface{}
func structToMap(obj interface{}) map[string]interface{} {
	out := make(map[string]interface{})
	j, _ := json.Marshal(obj)
	json.Unmarshal(j, &out)
	return out
}

// 修改函数的返回类型为 *Processor
func NewProcessor(settings *config.Settings, wsclient []*wsclient.WebSocketClient) *Processors {
	return &Processors{
		Settings: settings,
		Wsclient: wsclient,
	}
}

// 修改函数的返回类型为 *Processor
func NewProcessorV2(settings *config.Settings) *Processors {
	return &Processors{

		Settings: settings,
	}
}

// 发信息给所有连接正向ws的客户端
func (p *Processors) SendMessageToAllClients(message map[string]interface{}) error {
	var result *multierror.Error

	for _, client := range p.WsServerClients {
		// 使用接口的方法
		err := client.SendMessage(message)
		if err != nil {
			// Append the error to our result
			result = multierror.Append(result, fmt.Errorf("failed to send to client: %w", err))
		}
	}

	// This will return nil if no errors were added
	return result.ErrorOrNil()
}

// 方便快捷的发信息函数
func (p *Processors) BroadcastMessageToAll(message map[string]interface{}) error {
	var errors []string

	// 发送到我们作为客户端的Wsclient
	for _, client := range p.Wsclient {
		err := client.SendMessage(message)
		if err != nil {
			errors = append(errors, fmt.Sprintf("error sending private message via wsclient: %v", err))
		}
	}

	// 发送到我们作为服务器连接到我们的WsServerClients
	for _, serverClient := range p.WsServerClients {
		err := serverClient.SendMessage(message)
		if err != nil {
			errors = append(errors, fmt.Sprintf("error sending private message via WsServerClient: %v", err))
		}
	}

	// 在循环结束后处理记录的错误
	if len(errors) > 0 {
		return fmt.Errorf(strings.Join(errors, "; "))
	}

	//判断是否填写了反向post地址
	if !allEmpty(config.GetPostUrl()) {
		PostMessageToUrls(message)
	}

	return nil
}

// allEmpty checks if all the strings in the slice are empty.
func allEmpty(addresses []string) bool {
	for _, addr := range addresses {
		if addr != "" {
			return false
		}
	}
	return true
}

// 上报信息给反向Http
func PostMessageToUrls(message map[string]interface{}) {
	// 获取上报 URL 列表
	postUrls := config.GetPostUrl()

	// 检查 postUrls 是否为空
	if len(postUrls) > 0 {

		// 转换 message 为 JSON 字符串
		jsonString, err := handlers.ConvertMapToJSONString(message)
		if err != nil {
			mylog.Printf("Error converting message to JSON: %v", err)
			return
		}

		for _, url := range postUrls {
			// 创建请求体
			reqBody := bytes.NewBufferString(jsonString)

			// 创建 POST 请求
			req, err := http.NewRequest("POST", url, reqBody)
			if err != nil {
				mylog.Printf("Error creating POST request to %s: %v", url, err)
				continue
			}

			// 设置请求头
			req.Header.Set("Content-Type", "application/json")
			// 设置 X-Self-ID
			selfid := config.GetAppIDStr()
			req.Header.Set("X-Self-ID", selfid)

			// 发送请求
			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				mylog.Printf("Error sending POST request to %s: %v", url, err)
				continue
			}

			// 处理响应
			defer resp.Body.Close()
			// 可以添加更多的响应处理逻辑，如检查状态码等

			mylog.Printf("Posted to %s successfully", url)
		}
	}
}

func (p *Processors) HandleFrameworkCommand(messageText string, data interface{}, Type string, s *discordgo.Session) error {
	// 正则表达式匹配转换后的 CQ 码
	cqRegex := regexp.MustCompile(`\[CQ:at,qq=\d+\]`)

	// 使用正则表达式替换所有的 CQ 码为 ""
	cleanedMessage := cqRegex.ReplaceAllString(messageText, "")

	// 去除字符串前后的空格
	cleanedMessage = strings.TrimSpace(cleanedMessage)
	if cleanedMessage == "t" {
		// 生成临时指令
		tempCmd := handleNoPermission()
		mylog.Printf("临时bind指令: %s 可忽略权限检查1次,或将masterid设置为空数组", tempCmd)
	}
	var err error
	var now, new, newpro1, newpro2 string
	var realid, realid2 string
	var guilduserid string
	switch v := data.(type) {
	case *discordgo.MessageCreate:
		realid = v.Author.ID
		guilduserid = v.Author.ID
	}

	switch v := data.(type) {
	case *discordgo.MessageCreate:
		realid2 = v.ChannelID
	}

	//获取MasterID数组
	masterIDs := config.GetMasterID()
	//根据realid获取new
	now, new, err = idmap.RetrieveVirtualValuev2(realid)
	if config.GetIdmapPro() {
		newpro1, newpro2, err = idmap.RetrieveVirtualValuev2Pro(realid2, realid)
	}
	// 检查真实值或虚拟值是否在数组中
	var realValueIncluded, virtualValueIncluded bool

	//如果 masterIDs 数组为空，则这两个值恒为 true
	if len(masterIDs) == 0 {
		realValueIncluded = true
		virtualValueIncluded = true
	} else {
		// 否则，检查真实值或虚拟值是否在数组中
		realValueIncluded = contains(masterIDs, realid)
		virtualValueIncluded = contains(masterIDs, new)
	}

	//unlock指令
	if Type == "guild" && strings.HasPrefix(cleanedMessage, config.GetUnlockPrefix()) {
		// 获取用户ID
		userID := guilduserid

		// 创建一个与用户的私信频道
		dmChannel, err := s.UserChannelCreate(userID)
		if err != nil {
			mylog.Printf("unlock指令创建dm失败: %v", err)
			return err
		}

		// 准备发送的消息
		messageText := "欢迎使用Gensokyo框架部署QQ机器人"

		// 创建消息体
		msg := &discordgo.MessageSend{
			Content: messageText,
		}

		// 向私信频道发送消息
		_, err = s.ChannelMessageSendComplex(dmChannel.ID, msg)
		if err != nil {
			mylog.Printf("unlock指令发送失败: %v", err)
			return err
		}
	}

	// me指令处理逻辑
	if strings.HasPrefix(cleanedMessage, config.GetMePrefix()) {
		if err != nil {
			// 发送错误信息
			SendMessage(err.Error(), data, Type, s)
			return err
		}
		// 发送成功信息
		if config.GetIdmapPro() {
			// 构造清晰的对应关系信息
			userMapping := fmt.Sprintf("当前真实值（用户）/当前虚拟值（用户） = [%s/%s]", realid, newpro2)
			groupMapping := fmt.Sprintf("当前真实值（群/频道）/当前虚拟值（群/频道） = [%s/%s]", realid2, newpro1)

			// 构造 bind 指令的使用说明
			bindInstruction := fmt.Sprintf("bind 指令: %s 当前虚拟值(用户) 目标虚拟值(用户) [当前虚拟值(群/频道) 目标虚拟值(群/频道)]", config.GetBindPrefix())

			// 发送整合后的消息
			message := fmt.Sprintf("idmaps-pro状态:\n%s\n%s\n%s", userMapping, groupMapping, bindInstruction)
			SendMessage(message, data, Type, s)
		} else {
			SendMessage("目前状态:\n当前真实值 "+now+"\n当前虚拟值 "+new+"\nbind指令:"+config.GetBindPrefix()+" 当前虚拟值"+" 目标虚拟值", data, Type, s)
		}
		return nil
	}

	fields := strings.Fields(cleanedMessage)

	// 首先确保消息不是空的，然后检查是否是有效的临时指令
	if len(fields) > 0 && isValidTemporaryCommand(fields[0]) {
		// 执行 bind 操作
		if config.GetIdmapPro() {
			err := performBindOperationV2(cleanedMessage, data, Type, newpro1, s)
			if err != nil {
				mylog.Printf("bind遇到错误:%v", err)
			}
		} else {
			err := performBindOperation(cleanedMessage, data, Type, s)
			if err != nil {
				mylog.Printf("bind遇到错误:%v", err)
			}
		}
		return nil
	}

	// // 如果不是临时指令，检查是否具有执行bind操作的权限并且消息以特定前缀开始
	if (realValueIncluded || virtualValueIncluded) && strings.HasPrefix(cleanedMessage, config.GetBindPrefix()) {
		// 执行 bind 操作
		if config.GetIdmapPro() {
			err := performBindOperationV2(cleanedMessage, data, Type, newpro1, s)
			if err != nil {
				mylog.Printf("bind遇到错误:%v", err)
			}
		} else {
			err := performBindOperation(cleanedMessage, data, Type, s)
			if err != nil {
				mylog.Printf("bind遇到错误:%v", err)
			}
		}
		return nil
	} else if strings.HasPrefix(cleanedMessage, config.GetBindPrefix()) {
		// 生成临时指令
		tempCmd := handleNoPermission()
		mylog.Printf("您没有权限,使用临时指令：%s 忽略权限检查,或将masterid设置为空数组", tempCmd)
		SendMessage("您没有权限,请配置config.yml或查看日志,使用临时指令", data, Type, s)
	}
	return nil
}

// 生成由两个英文字母构成的唯一临时指令
func generateTemporaryCommand() (string, error) {
	bytes := make([]byte, 1) // 生成1字节的随机数，足以表示2个十六进制字符
	if _, err := rand.Read(bytes); err != nil {
		return "", err // 处理随机数生成错误
	}
	command := hex.EncodeToString(bytes)[:2] // 将1字节转换为2个十六进制字符
	return command, nil
}

// 生成并添加一个新的临时指令
func handleNoPermission() string {
	idmap.MutexT.Lock()
	defer idmap.MutexT.Unlock()

	cmd, _ := generateTemporaryCommand()
	idmap.TemporaryCommands = append(idmap.TemporaryCommands, cmd)
	return cmd
}

// 检查指令是否是有效的临时指令
func isValidTemporaryCommand(cmd string) bool {
	idmap.MutexT.Lock()
	defer idmap.MutexT.Unlock()

	for i, tempCmd := range idmap.TemporaryCommands {
		if tempCmd == cmd {
			// 删除已验证的临时指令
			idmap.TemporaryCommands = append(idmap.TemporaryCommands[:i], idmap.TemporaryCommands[i+1:]...)
			return true
		}
	}
	return false
}

// 执行 bind 操作的逻辑
func performBindOperation(cleanedMessage string, data interface{}, Type string, s *discordgo.Session) error {
	// 分割指令以获取参数
	parts := strings.Fields(cleanedMessage)
	if len(parts) != 3 {
		mylog.Printf("bind指令参数错误\n正确的格式" + config.GetBindPrefix() + " 当前虚拟值 新虚拟值")
		return nil
	}

	// 将字符串转换为 int64
	oldRowValue, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return err
	}

	newRowValue, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return err
	}

	// 调用 UpdateVirtualValue
	err = idmap.UpdateVirtualValuev2(oldRowValue, newRowValue)
	if err != nil {
		SendMessage(err.Error(), data, Type, s)
		return err
	}
	now, new, err := idmap.RetrieveRealValuev2(newRowValue)
	if err != nil {
		SendMessage(err.Error(), data, Type, s)
	} else {
		SendMessage("绑定成功,目前状态:\n当前真实值 "+now+"\n当前虚拟值 "+new, data, Type, s)
	}

	return nil
}

func performBindOperationV2(cleanedMessage string, data interface{}, Type string, GroupVir string, s *discordgo.Session) error {
	// 分割指令以获取参数
	parts := strings.Fields(cleanedMessage)

	// 检查参数数量
	if len(parts) < 3 || len(parts) > 5 {
		mylog.Printf("bind指令参数错误\n正确的格式: " + config.GetBindPrefix() + " 当前虚拟值(用户) 新虚拟值(用户) [当前虚拟值(群) 新虚拟值(群)]")
		return nil
	}

	// 当前虚拟值 用户
	oldVirtualValue1, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return err
	}
	//新的虚拟值 用户
	newVirtualValue1, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return err
	}

	// 设置默认值
	var oldRowValue, newRowValue int64

	// 如果提供了第3个和第4个参数，则解析它们
	if len(parts) > 3 {
		oldRowValue, err = parseOrDefault(parts[3], GroupVir)
		if err != nil {
			return err
		}

		newRowValue, err = parseOrDefault(parts[4], GroupVir)
		if err != nil {
			return err
		}
	} else {
		// 如果没有提供这些参数，则直接使用 GroupVir
		oldRowValue, err = strconv.ParseInt(GroupVir, 10, 64)
		if err != nil {
			return err
		}
		newRowValue = oldRowValue // 使用相同的值
	}
	// 调用 UpdateVirtualValue(兼顾老转换)
	err = idmap.UpdateVirtualValuev2(oldVirtualValue1, newVirtualValue1)
	if err != nil {
		SendMessage(err.Error(), data, Type, s)
		return err
	}
	// 调用 UpdateVirtualValuev2Pro
	err = idmap.UpdateVirtualValuev2Pro(oldRowValue, newRowValue, oldVirtualValue1, newVirtualValue1)
	if err != nil {
		SendMessage(err.Error(), data, Type, s)
		return err
	}

	now, new, err := idmap.RetrieveRealValuesv2Pro(newRowValue, newVirtualValue1)
	if err != nil {
		SendMessage(err.Error(), data, Type, s)
	} else {
		newVirtualValue1Str := strconv.FormatInt(newRowValue, 10)
		newVirtualValue2Str := strconv.FormatInt(newVirtualValue1, 10)
		SendMessage("绑定成功,目前状态:\n当前真实值(群)"+now+"\n当前真实值(用户)"+new+"\n当前虚拟值(群)"+newVirtualValue1Str+"当前虚拟值(用户)"+newVirtualValue2Str, data, Type, s)
	}

	return nil
}

// parseOrDefault 将字符串转换为int64，如果无法转换或为0，则使用默认值
func parseOrDefault(s string, defaultValue string) (int64, error) {
	value, err := strconv.ParseInt(s, 10, 64)
	if err == nil && value != 0 {
		return value, nil
	}

	return strconv.ParseInt(defaultValue, 10, 64)
}

// contains 检查数组中是否包含指定的字符串
func contains(slice []string, item string) bool {
	for _, a := range slice {
		if a == item {
			return true
		}
	}
	return false
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

		combinedMsg, err := handlers.GenerateReplyMessage(foundItems, messageText)
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

		combinedMsg, err := handlers.GenerateReplyMessage(foundItems, messageText)
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
