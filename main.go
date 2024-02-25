package main

import (
	"bufio"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/fatih/color"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/hoshinonyaruko/gensokyo-discord/Processor"
	"github.com/hoshinonyaruko/gensokyo-discord/config"
	"github.com/hoshinonyaruko/gensokyo-discord/handlers"
	"github.com/hoshinonyaruko/gensokyo-discord/httpapi"
	"github.com/hoshinonyaruko/gensokyo-discord/idmap"
	"github.com/hoshinonyaruko/gensokyo-discord/mylog"
	"github.com/hoshinonyaruko/gensokyo-discord/server"
	"github.com/hoshinonyaruko/gensokyo-discord/shorturl"
	"github.com/hoshinonyaruko/gensokyo-discord/sys"
	"github.com/hoshinonyaruko/gensokyo-discord/template"
	"github.com/hoshinonyaruko/gensokyo-discord/webui"
	"github.com/hoshinonyaruko/gensokyo-discord/wsclient"
)

type Event interface{}

// 消息处理器
var p *Processor.Processors
var globalBotId string

func main() {
	// 定义faststart命令行标志。默认为false。
	fastStart := flag.Bool("faststart", false, "start without initialization if set")

	// 解析命令行参数到定义的标志。
	flag.Parse()

	// 检查是否使用了-faststart参数
	if !*fastStart {
		sys.InitBase() // 如果不是faststart模式，则执行初始化
	}
	if _, err := os.Stat("config.yml"); os.IsNotExist(err) {
		var ip string
		var err error
		// 检查操作系统是否为Android
		if runtime.GOOS == "android" {
			ip = "127.0.0.1"
		} else {
			// 获取内网IP地址
			ip, err = sys.GetLocalIP()
			if err != nil {
				mylog.Println("Error retrieving the local IP address:", err)
				ip = "127.0.0.1"
			}
		}
		// 将 <YOUR_SERVER_DIR> 替换成实际的内网IP地址 确保初始状态webui能够被访问
		configData := strings.Replace(template.ConfigTemplate, "<YOUR_SERVER_DIR>", ip, -1)

		// 将修改后的配置写入 config.yml
		err = os.WriteFile("config.yml", []byte(configData), 0644)
		if err != nil {
			mylog.Println("Error writing config.yml:", err)
			return
		}

		mylog.Println("请配置config.yml然后再次运行.")
		mylog.Printf("按下 Enter 继续...")
		bufio.NewReader(os.Stdin).ReadBytes('\n')
		os.Exit(0)
	}

	// 主逻辑
	// 加载配置
	conf, err := config.LoadConfig("config.yml")
	if err != nil {
		mylog.Fatalf("error: %v", err)
	}
	sys.SetTitle(conf.Settings.Title)
	webuiURL := config.ComposeWebUIURL(conf.Settings.Lotus)     // 调用函数获取URL
	webuiURLv2 := config.ComposeWebUIURLv2(conf.Settings.Lotus) // 调用函数获取URL

	var wsClients []*wsclient.WebSocketClient
	var nologin bool
	var dg *discordgo.Session

	if conf.Settings.AppID == 12345 {
		// 输出天蓝色文本
		cyan := color.New(color.FgCyan)
		cyan.Printf("欢迎来到Gensokyo, 控制台地址: %s\n", webuiURL)
		mylog.Println("请修改config,将appid修改为不为12345,后重启框架。")
		mylog.Println("请修改config,将appid修改为不为12345,后重启框架。")
		mylog.Println("并且到discord developer网站,获取机器人的token并填入config")
	} else {
		// 创建一个新的 Discord 会话
		dg, err = discordgo.New("Bot " + conf.Settings.Token)
		if err != nil {
			mylog.Printf("创建 Discord 会话时出错: %v\n", err)
			return // 或其他错误处理
		}

		// 设置代理
		if conf.Settings.ProxyAdress != "" {
			proxyURL, err := url.Parse(conf.Settings.ProxyAdress)
			if err != nil {
				mylog.Printf("解析代理 URL 时出错: %v\n", err)
				return // 或其他错误处理
			}
			httpClient := &http.Client{
				Transport: &http.Transport{
					Proxy: http.ProxyURL(proxyURL),
				},
				Timeout: 20 * time.Second,
			}
			dg.Client = httpClient
			// 设置 WebSocket 的代理
			dialer := *websocket.DefaultDialer
			dialer.Proxy = http.ProxyURL(proxyURL)
			dg.Dialer = &dialer
		}
		// 订阅 Intents
		registerHandlersFromConfig(dg, conf.Settings.TextIntent)
		configURL := config.GetDevelop_Acdir()
		// 开始监听
		err = dg.Open()
		if err != nil {
			mylog.Printf("打开 Discord 连接时出错: %v\n", err)
			mylog.Printf("请检查代理设置,proxy_adress项目,切换线路并重试\n")
			nologin = true
			return // 或其他错误处理
		}
		if !nologin {
			if configURL == "" { //初始化handlers
				handlers.BotID = dg.State.User.ID
				mylog.Printf("本机器人id:%v\n", handlers.BotID)
				globalBotId = dg.State.User.ID
			} else { //初始化handlers
				handlers.BotID = config.GetDevBotid()
			}

			handlers.AppID = fmt.Sprintf("%d", conf.Settings.AppID)
			mylog.Printf("本机器人心跳时的id将会是(取决于config设置的appid):%v\n", handlers.AppID)

			// 启动多个WebSocket客户端的逻辑
			if !allEmpty(conf.Settings.WsAddress) {
				wsClientChan := make(chan *wsclient.WebSocketClient, len(conf.Settings.WsAddress))
				errorChan := make(chan error, len(conf.Settings.WsAddress))
				// 定义计数器跟踪尝试建立的连接数
				attemptedConnections := 0
				for _, wsAddr := range conf.Settings.WsAddress {
					if wsAddr == "" {
						continue // Skip empty addresses
					}
					attemptedConnections++ // 增加尝试连接的计数
					go func(address string) {
						retry := config.GetLaunchReconectTimes()
						wsClient, err := wsclient.NewWebSocketClient(address, conf.Settings.AppID, dg, retry)
						if err != nil {
							mylog.Printf("Error creating WebSocketClient for address(连接到反向ws失败) %s: %v\n", address, err)
							errorChan <- err
							return
						}
						wsClientChan <- wsClient
					}(wsAddr)
				}
				// 获取连接成功后的wsClient
				for i := 0; i < attemptedConnections; i++ {
					select {
					case wsClient := <-wsClientChan:
						wsClients = append(wsClients, wsClient)
					case err := <-errorChan:
						mylog.Printf("Error encountered while initializing WebSocketClient: %v\n", err)
					}
				}

				// 确保所有尝试建立的连接都有对应的wsClient
				if len(wsClients) != attemptedConnections {
					mylog.Println("Error: Not all wsClients are initialized!(反向ws未设置或连接失败)")
					// 处理初始化失败的情况
					p = Processor.NewProcessorV2(&conf.Settings)
					//只启动正向
				} else {
					mylog.Println("All wsClients are successfully initialized.")
					// 所有客户端都成功初始化
					p = Processor.NewProcessor(&conf.Settings, wsClients)
				}
			} else if conf.Settings.EnableWsServer {
				mylog.Println("只启动正向ws")
				p = Processor.NewProcessorV2(&conf.Settings)
			}
		} else {
			// 设置颜色为红色
			red := color.New(color.FgRed)
			// 输出红色文本
			red.Println("请设置正确的appid、token、clientsecret再试")
		}
	}

	//创建idmap服务器 数据库
	idmap.InitializeDB()
	//创建webui数据库
	webui.InitializeDB()
	defer idmap.CloseDB()
	defer webui.CloseDB()

	//logger
	//logLevel := mylog.GetLogLevelFromConfig(config.GetLogLevel())
	//loggerAdapter := mylog.NewlogAdapter(logLevel, config.GetSaveLogs())
	//todo 实现dc的日志分级

	//图片上传 调用次数限制
	rateLimiter := server.NewRateLimiter()
	// 根据 lotus 的值选择端口
	var serverPort string
	if !conf.Settings.Lotus {
		serverPort = conf.Settings.Port
	} else {
		serverPort = conf.Settings.BackupPort
	}
	var r *gin.Engine
	var hr *gin.Engine
	if config.GetDeveloperLog() { // 是否启动调试状态
		r = gin.Default()
		hr = gin.Default()
	} else {
		r = gin.New()
		r.Use(gin.Recovery()) // 添加恢复中间件，但不添加日志中间件
		hr = gin.New()
		hr.Use(gin.Recovery())
	}
	r.GET("/getid", server.GetIDHandler)
	r.GET("/updateport", server.HandleIpupdate)
	r.POST("/uploadpic", server.UploadBase64ImageHandler(rateLimiter))
	r.POST("/uploadrecord", server.UploadBase64RecordHandler(rateLimiter))
	r.Static("/channel_temp", "./channel_temp")
	if config.GetFrpPort() == "0" {
		//webui和它的api
		webuiGroup := r.Group("/webui")
		{
			webuiGroup.GET("/*filepath", webui.CombinedMiddleware(dg))
			webuiGroup.POST("/*filepath", webui.CombinedMiddleware(dg))
			webuiGroup.PUT("/*filepath", webui.CombinedMiddleware(dg))
			webuiGroup.DELETE("/*filepath", webui.CombinedMiddleware(dg))
			webuiGroup.PATCH("/*filepath", webui.CombinedMiddleware(dg))
		}
	}
	//正向http api
	http_api_address := config.GetHttpAddress()
	if http_api_address != "" {
		mylog.Println("正向http api启动成功,监听" + http_api_address + "若有需要,请对外放通端口...")
		HttpApiGroup := hr.Group("/")
		{
			HttpApiGroup.GET("/*filepath", httpapi.CombinedMiddleware(dg))
			HttpApiGroup.POST("/*filepath", httpapi.CombinedMiddleware(dg))
			HttpApiGroup.PUT("/*filepath", httpapi.CombinedMiddleware(dg))
			HttpApiGroup.DELETE("/*filepath", httpapi.CombinedMiddleware(dg))
			HttpApiGroup.PATCH("/*filepath", httpapi.CombinedMiddleware(dg))
		}
	}
	//正向ws
	if conf.Settings.AppID != 12345 {
		if conf.Settings.EnableWsServer {
			wspath := config.GetWsServerPath()
			if wspath == "nil" {
				r.GET("", server.WsHandlerWithDependencies(dg, p))
				mylog.Println("正向ws启动成功,监听0.0.0.0:" + serverPort + "请注意设置ws_server_token(可空),并对外放通端口...")
			} else {
				r.GET("/"+wspath, server.WsHandlerWithDependencies(dg, p))
				mylog.Println("正向ws启动成功,监听0.0.0.0:" + serverPort + "/" + wspath + "请注意设置ws_server_token(可空),并对外放通端口...")
			}
		}
	}
	r.POST("/url", shorturl.CreateShortURLHandler)
	r.GET("/url/:shortURL", shorturl.RedirectFromShortURLHandler)
	if config.GetIdentifyFile() {
		appIDStr := config.GetAppIDStr()
		fileName := appIDStr + ".json"
		r.GET("/"+fileName, func(c *gin.Context) {
			content := fmt.Sprintf(`{"bot_appid":%d}`, config.GetAppID())
			c.Header("Content-Type", "application/json")
			c.String(200, content)
		})
	}
	// 创建一个http.Server实例（主服务器）
	httpServer := &http.Server{
		Addr:    "0.0.0.0:" + serverPort,
		Handler: r,
	}
	mylog.Printf("gin运行在%v端口", serverPort)
	// 在一个新的goroutine中启动主服务器
	go func() {
		if serverPort == "443" {
			// 使用HTTPS
			crtPath := config.GetCrtPath()
			keyPath := config.GetKeyPath()
			if crtPath == "" || keyPath == "" {
				mylog.Fatalf("crt or key path is missing for HTTPS")
				return
			}
			if err := httpServer.ListenAndServeTLS(crtPath, keyPath); err != nil && err != http.ErrServerClosed {
				mylog.Fatalf("listen (HTTPS): %s\n", err)
			}
		} else {
			// 使用HTTP
			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				mylog.Fatalf("listen: %s\n", err)
			}
		}
	}()

	// 如果主服务器使用443端口，同时在一个新的goroutine中启动444端口的HTTP服务器 todo 更优解
	if serverPort == "443" {
		go func() {
			// 创建另一个http.Server实例（用于444端口）
			httpServer444 := &http.Server{
				Addr:    "0.0.0.0:444",
				Handler: r,
			}

			// 启动444端口的HTTP服务器
			if err := httpServer444.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				mylog.Fatalf("listen (HTTP 444): %s\n", err)
			}
		}()
	}
	// 创建 httpapi 的http server
	if http_api_address != "" {
		go func() {
			// 创建一个http.Server实例（Http Api服务器）
			httpServerHttpApi := &http.Server{
				Addr:    http_api_address,
				Handler: hr,
			}
			// 使用HTTP
			if err := httpServerHttpApi.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				mylog.Fatalf("http apilisten: %s\n", err)
			}
		}()
	}

	// 使用color库输出天蓝色的文本
	cyan := color.New(color.FgCyan)
	cyan.Printf("欢迎来到Gensokyo, 控制台地址: %s\n", webuiURL)
	cyan.Printf("%s\n", template.Logo)
	cyan.Printf("欢迎来到Gensokyo, 公网控制台地址(需开放端口): %s\n", webuiURLv2)

	// Wait here until CTRL-C or other term signal is received.
	fmt.Println("Bot is now running. Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-sc

	// Cleanly close down the Discord session.
	dg.Close()
}

func guildsHandler(s *discordgo.Session, i interface{}) {
	event, ok := i.(*discordgo.GuildCreate)
	if !ok {
		//mylog.Println("Event type mismatch: expected *discordgo.GuildCreate")
		return
	}

	// 处理 GuildCreate 事件
	mylog.Printf("New guild created: %s", event.Name)
}

func guildMembersHandler(s *discordgo.Session, i interface{}) {
	event, ok := i.(*discordgo.GuildMemberAdd)
	if !ok {
		//mylog.Println("Event type mismatch: expected *discordgo.GuildMemberAdd")
		return
	}

	// 处理 GuildMemberAdd 事件
	mylog.Printf("New member added: %s", event.Member.User.Username)
}

func guildBansHandler(s *discordgo.Session, i interface{}) {
	event, ok := i.(*discordgo.GuildBanAdd)
	if !ok {
		//mylog.Println("Event type mismatch: expected *discordgo.GuildBanAdd")
		return
	}

	// 处理 GuildBanAdd 事件
	mylog.Printf("Member banned: %s", event.User.Username)
}

func guildEmojisHandler(s *discordgo.Session, i interface{}) {
	event, ok := i.(*discordgo.GuildEmojisUpdate)
	if !ok {
		//mylog.Println("Event type mismatch: expected *discordgo.GuildEmojisUpdate")
		return
	}

	// 处理 GuildEmojisUpdate 事件
	mylog.Printf("Guild emojis updated in guild: %s", event.GuildID)
}

func guildIntegrationsHandler(s *discordgo.Session, i interface{}) {
	event, ok := i.(*discordgo.GuildIntegrationsUpdate)
	if !ok {
		//mylog.Println("Event type mismatch: expected *discordgo.GuildIntegrationsUpdate")
		return
	}

	// 处理 GuildIntegrationsUpdate 事件
	mylog.Printf("Guild integrations updated in guild: %s", event.GuildID)
}

func guildWebhooksHandler(s *discordgo.Session, i interface{}) {
	event, ok := i.(*discordgo.WebhooksUpdate)
	if !ok {
		//mylog.Println("Event type mismatch: expected *discordgo.WebhooksUpdate")
		return
	}

	// 处理 WebhooksUpdate 事件
	mylog.Printf("Webhooks updated in channel: %s of guild: %s", event.ChannelID, event.GuildID)
}

func guildInvitesHandler(s *discordgo.Session, i interface{}) {
	event, ok := i.(*discordgo.InviteCreate)
	if !ok {
		//mylog.Println("Event type mismatch: expected *discordgo.InviteCreate")
		return
	}

	// 处理 InviteCreate 事件
	mylog.Printf("New invite created: %s in guild: %s", event.Code, event.GuildID)
}

func guildVoiceStatesHandler(s *discordgo.Session, i interface{}) {
	event, ok := i.(*discordgo.VoiceStateUpdate)
	if !ok {
		//mylog.Println("Event type mismatch: expected *discordgo.VoiceStateUpdate")
		return
	}

	// 处理 VoiceStateUpdate 事件
	mylog.Printf("Voice state updated in guild: %s", event.GuildID)
}

func guildPresencesHandler(s *discordgo.Session, i interface{}) {
	event, ok := i.(*discordgo.PresenceUpdate)
	if !ok {
		//mylog.Println("Event type mismatch: expected *discordgo.PresenceUpdate")
		return
	}

	// 处理 PresenceUpdate 事件
	mylog.Printf("Presence updated: %s in guild: %s", event.User.ID, event.GuildID)
}

// 根据配置动态注册 Handlers
func registerHandlersFromConfig(dg *discordgo.Session, intents []string) {
	for _, intentName := range intents {
		handler := mapIntentToHandler(intentName)
		if handler == nil {
			//mylog.Printf("Handler not found for intent: %s\n", intentName)
			continue
		}
		// 注册 Handler
		dg.AddHandler(handler)
	}
}

func guildMessagesHandler(s *discordgo.Session, i interface{}) {
	event, ok := i.(*discordgo.MessageCreate)
	if !ok {
		// mylog.Println("Event type mismatch: expected *discordgo.MessageCreate")
		return
	}
	if event.Author.ID == globalBotId {
		return
	}
	// 如果 GuildID 为空，则认为是私信
	if event.GuildID == "" {
		// 处理私信
		mylog.Printf("New direct message: content: %s", event.Content)
		p.ProcessChannelDirectMessage(event, s)
	} else {
		// 否则，处理公会消息
		mylog.Printf("New message in guild: %s, channel: %s, content: %s", event.GuildID, event.ChannelID, event.Content)
		p.ProcessGuildNormalMessage(event, s)
	}
}

func guildMessageReactionsHandler(s *discordgo.Session, i interface{}) {
	// event, ok := i.(*discordgo.MessageReactionAdd)
	// if !ok {
	// 	mylog.Println("Event type mismatch: expected *discordgo.MessageReactionAdd")
	// 	return
	// }

	// 处理 MessageReactionAdd 事件
	// 例如: mylog.Printf("New reaction added to message: %s in channel: %s", event.MessageID, event.ChannelID)
}

func guildMessageTypingHandler(s *discordgo.Session, i interface{}) {
	event, ok := i.(*discordgo.TypingStart)
	if !ok {
		//mylog.Println("Event type mismatch: expected *discordgo.TypingStart")
		return
	}

	// 处理 TypingStart 事件
	mylog.Printf("User is typing in channel: %s", event.ChannelID)
}
func directMessagesHandler(s *discordgo.Session, i interface{}) {
	event, ok := i.(*discordgo.MessageCreate)
	if !ok {
		//mylog.Println("Event type mismatch: expected *discordgo.MessageCreate")
		return
	}

	// 只处理私人消息
	if event.GuildID != "" {
		return // 不是私人消息
	}

	// 处理私人消息
	// 例如: mylog.Printf("New direct message from user: %s, content: %s", event.Author.ID, event.Content)
}
func directMessageReactionsHandler(s *discordgo.Session, i interface{}) {
	event, ok := i.(*discordgo.MessageReactionAdd)
	if !ok {
		//mylog.Println("Event type mismatch: expected *discordgo.MessageReactionAdd")
		return
	}

	// 只处理私人消息的反应
	// 这需要检查消息是否来自私人频道
	// ...

	// 处理私人消息的反应
	mylog.Printf("New reaction added to direct message: %s", event.MessageID)
}
func directMessageTypingHandler(s *discordgo.Session, i interface{}) {
	event, ok := i.(*discordgo.TypingStart)
	if !ok {
		//mylog.Println("Event type mismatch: expected *discordgo.TypingStart")
		return
	}

	// 只处理私人消息的打字状态
	// 这需要检查打字状态是否在私人频道中
	// ...

	// 处理私人消息的打字状态
	mylog.Printf("User is typing a direct message: %s", event.UserID)
}
func messageContentHandler(s *discordgo.Session, i interface{}) {
	event, ok := i.(*discordgo.MessageCreate)
	if !ok {
		//mylog.Println("Event type mismatch: expected *discordgo.MessageCreate")
		return
	}

	// 处理消息内容
	mylog.Printf("New message content: %s", event.Content)
}
func guildScheduledEventsHandler(s *discordgo.Session, i interface{}) {
	event, ok := i.(*discordgo.GuildScheduledEventCreate)
	if !ok {
		//mylog.Println("Event type mismatch: expected *discordgo.GuildScheduledEventCreate")
		return
	}

	// 处理计划事件的创建
	mylog.Printf("New scheduled event created: %s", event.Name)
}

// 将 Discord Intent 名称映射到相应的 Handler 函数
func mapIntentToHandler(intentName string) func(*discordgo.Session, interface{}) {
	switch intentName {
	case "Guilds":
		return guildsHandler
	case "GuildMembers":
		return guildMembersHandler
	case "GuildBans":
		return guildBansHandler
	case "GuildEmojis":
		return guildEmojisHandler
	case "GuildIntegrations":
		return guildIntegrationsHandler
	case "GuildWebhooks":
		return guildWebhooksHandler
	case "GuildInvites":
		return guildInvitesHandler
	case "GuildVoiceStates":
		return guildVoiceStatesHandler
	case "GuildPresences":
		return guildPresencesHandler
	case "GuildMessages":
		return guildMessagesHandler
	case "GuildMessageReactions":
		return guildMessageReactionsHandler
	case "GuildMessageTyping":
		return guildMessageTypingHandler
	case "DirectMessages":
		return directMessagesHandler
	case "DirectMessageReactions":
		return directMessageReactionsHandler
	case "DirectMessageTyping":
		return directMessageTypingHandler
	case "MessageContent":
		return messageContentHandler
	case "GuildScheduledEvents":
		return guildScheduledEventsHandler
	default:
		mylog.Printf("Unknown intent: %s\n", intentName)
		return nil
	}
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
