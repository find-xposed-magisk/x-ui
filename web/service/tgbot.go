package service

import (
	"context"
	"embed"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alireza0/x-ui/config"
	"github.com/alireza0/x-ui/database"
	"github.com/alireza0/x-ui/database/model"
	"github.com/alireza0/x-ui/logger"
	"github.com/alireza0/x-ui/util/common"
	"github.com/alireza0/x-ui/util/proxyclient"
	"github.com/alireza0/x-ui/util/tgchat"
	"github.com/alireza0/x-ui/web/locale"
	"github.com/alireza0/x-ui/xray"

	tgbotapi "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

var (
	bot        *tgbotapi.Bot
	botCancel  context.CancelFunc
	adminChats []tgchat.Chat
	notifyOnly bool
	isRunning  bool
	hostname   string
)

// pollTimeout is how long a long-polling request waits for an update before
// the library issues a new one.
const pollTimeout = 10 * time.Second

// botCtx bounds one outgoing API call. Long polling runs on its own context, so
// stopping the receiver never cancels a report in flight.
func botCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

// uploadCtx bounds a file upload. A backup of a busy panel's database is orders
// of magnitude bigger than a message and may cross a slow tunnel, so it cannot
// share the message deadline.
func uploadCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Minute)
}

type LoginStatus byte

const (
	LoginSuccess LoginStatus = 1
	LoginFail    LoginStatus = 0
)

type Tgbot struct {
	inboundService InboundService
	settingService SettingService
	serverService  ServerService
	lastStatus     *Status
}

func (t *Tgbot) NewTgbot() *Tgbot {
	return new(Tgbot)
}

func (t *Tgbot) I18nBot(name string, params ...string) string {
	return locale.I18n(locale.Bot, name, params...)
}

func (t *Tgbot) Start(i18nFS embed.FS) error {
	err := locale.InitLocalizer(i18nFS, &t.settingService)
	if err != nil {
		return err
	}

	t.SetHostname()
	tgBottoken, err := t.settingService.GetTgBotToken()
	if err != nil || tgBottoken == "" {
		logger.Warning("Get TgBotToken failed:", err)
		return err
	}

	tgBotid, err := t.settingService.GetTgBotChatId()
	if err != nil {
		logger.Warning("Get GetTgBotChatId failed:", err)
		return err
	}

	adminChats, err = tgchat.Parse(tgBotid)
	if err != nil {
		logger.Warning("Failed to get IDs from GetTgBotChatId:", err)
		return err
	}

	// In notify-only mode the bot never answers anyone. A bot that replies to a
	// stranger's /start confirms both that the host runs a panel and which one,
	// so an admin who only wants outgoing reports can close that door entirely.
	notifyOnly, err = t.settingService.GetTgBotNotifyOnly()
	if err != nil {
		logger.Warning("Get TgBotNotifyOnly failed:", err)
		return err
	}

	// A panel inside a censored network usually cannot reach Telegram directly,
	// so every bot request may need to go through a proxy - typically one of the
	// panel's own inbounds listening on localhost.
	tgBotProxy, err := t.settingService.GetTgBotProxy()
	if err != nil {
		logger.Warning("Get TgBotProxy failed:", err)
		return err
	}
	httpClient, err := proxyclient.New(tgBotProxy)
	if err != nil {
		logger.Error("Tgbot proxy is not usable:", err)
		return err
	}
	if tgBotProxy != "" {
		logger.Info("Tgbot is using a proxy")
	}

	options := []tgbotapi.Option{
		tgbotapi.WithHTTPClient(pollTimeout, httpClient),
		tgbotapi.WithDefaultHandler(t.onUpdate),
	}
	if notifyOnly {
		// Without a handler the library would still long-poll; skipping the
		// receiver entirely is what makes the bot invisible to strangers.
		options = []tgbotapi.Option{tgbotapi.WithHTTPClient(pollTimeout, httpClient)}
	}

	for {
		bot, err = tgbotapi.New(tgBottoken, options...)
		if err != nil {
			fmt.Println("Get tgbot's api error:", err)
			fmt.Println("Retrying after 10 secound...")
			time.Sleep(10 * time.Second)
		} else {
			fmt.Println("Tgbot connected!")
			break
		}
	}

	if notifyOnly {
		logger.Info("Tgbot is in notify-only mode, incoming messages are ignored")
		isRunning = true
		return nil
	}

	// listen for TG bot income messages
	if !isRunning {
		logger.Info("Starting Telegram receiver ...")
		var ctx context.Context
		ctx, botCancel = context.WithCancel(context.Background())
		go bot.Start(ctx)
		isRunning = true
	}

	return nil
}

func (t *Tgbot) IsRunning() bool {
	return isRunning
}

func (t *Tgbot) SetHostname() {
	host, err := os.Hostname()
	if err != nil {
		logger.Error("get hostname error:", err)
		hostname = ""
		return
	}
	hostname = host
}

func (t *Tgbot) Stop() {
	if botCancel != nil {
		botCancel()
		botCancel = nil
		logger.Info("Stop Telegram receiver ...")
	}
	isRunning = false
	adminChats = nil
	notifyOnly = false
}

// onUpdate is called by the library for every incoming update.
func (t *Tgbot) onUpdate(ctx context.Context, _ *tgbotapi.Bot, update *models.Update) {
	if update == nil {
		return
	}
	if update.Message == nil {
		if update.CallbackQuery != nil {
			t.answerCallback(ctx, update.CallbackQuery)
		}
		return
	}
	if isCommand(update.Message) {
		chatId := update.Message.Chat.ID
		t.answerCommand(update.Message, chatId, checkAdmin(chatId))
	}
}

func (t *Tgbot) answerCommand(message *models.Message, chatId int64, isAdmin bool) {
	msg, onlyMessage := "", false

	command, commandArgs := commandOf(message), commandArgumentsOf(message)

	// Extract the command from the Message.
	switch command {
	case "help":
		msg += t.I18nBot("tgbot.commands.help")
		msg += t.I18nBot("tgbot.commands.pleaseChoose")
	case "start":
		msg += t.I18nBot("tgbot.commands.start", "Firstname=="+message.From.FirstName)
		if isAdmin {
			msg += t.I18nBot("tgbot.commands.welcome", "Hostname=="+hostname)
		}
		msg += "\n\n" + t.I18nBot("tgbot.commands.pleaseChoose")
	case "status":
		onlyMessage = true
		msg += t.I18nBot("tgbot.commands.status")
	case "id":
		onlyMessage = true
		msg += t.I18nBot("tgbot.commands.getID", "ID=="+strconv.FormatInt(message.From.ID, 10))
	case "usage":
		onlyMessage = true
		if len(commandArgs) > 1 {
			if isAdmin {
				t.searchClient(chatId, commandArgs)
			} else {
				t.searchForClient(chatId, commandArgs)
			}
		} else {
			msg += t.I18nBot("tgbot.commands.usage")
		}
	case "inbound":
		onlyMessage = true
		if isAdmin {
			t.searchInbound(chatId, commandArgs)
		} else {
			msg += t.I18nBot("tgbot.commands.unknown")
		}
	default:
		msg += t.I18nBot("tgbot.commands.unknown")
	}

	if onlyMessage {
		t.SendMsgToTgbot(chatId, msg)
		return
	}
	t.SendAnswer(chatId, msg, isAdmin)
}

func (t *Tgbot) answerCallback(ctx context.Context, callbackQuery *models.CallbackQuery) {
	// Respond to the callback query, telling Telegram to show the user
	// a message with the data received.
	_, err := bot.AnswerCallbackQuery(ctx, &tgbotapi.AnswerCallbackQueryParams{
		CallbackQueryID: callbackQuery.ID,
		Text:            callbackQuery.Data,
	})
	if err != nil {
		logger.Warning(err)
	}

	switch callbackQuery.Data {
	case "get_usage":
		t.SendMsgToTgbot(callbackQuery.From.ID, t.getServerUsage())
	case "inbounds":
		t.SendMsgToTgbot(callbackQuery.From.ID, t.getInboundUsages())
	case "deplete_soon":
		t.SendMsgToTgbot(callbackQuery.From.ID, t.getExhausted())
	case "get_backup":
		t.sendBackup(callbackQuery.From.ID)
	case "client_traffic":
		t.getClientUsage(callbackQuery.From.ID, callbackQuery.From.Username)
	case "client_commands":
		t.SendMsgToTgbot(callbackQuery.From.ID, t.I18nBot("tgbot.commands.helpClientCommands"))
	case "onlines":
		t.onlineClients(callbackQuery.From.ID)
	case "commands":
		t.SendMsgToTgbot(callbackQuery.From.ID, t.I18nBot("tgbot.commands.helpAdminCommands"))
	default:
		if email, found := strings.CutPrefix(callbackQuery.Data, "client_"); found {
			t.searchClient(callbackQuery.From.ID, email)
		}
	}
}

func checkAdmin(tgId int64) bool {
	for _, chat := range adminChats {
		if chat.ChatID == tgId {
			return true
		}
	}
	return false
}

func (t *Tgbot) SendAnswer(chatId int64, msg string, isAdmin bool) {
	numericKeyboard := models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: t.I18nBot("tgbot.buttons.serverUsage"), CallbackData: "get_usage"},
				{Text: t.I18nBot("tgbot.buttons.dbBackup"), CallbackData: "get_backup"},
			},
			{
				{Text: t.I18nBot("tgbot.buttons.getInbounds"), CallbackData: "inbounds"},
				{Text: t.I18nBot("tgbot.buttons.depleteSoon"), CallbackData: "deplete_soon"},
			},
			{
				{Text: t.I18nBot("tgbot.buttons.commands"), CallbackData: "commands"},
				{Text: t.I18nBot("tgbot.buttons.onlines"), CallbackData: "onlines"},
			},
		},
	}
	numericKeyboardClient := models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: t.I18nBot("tgbot.buttons.clientUsage"), CallbackData: "client_traffic"},
				{Text: t.I18nBot("tgbot.buttons.commands"), CallbackData: "client_commands"},
			},
		},
	}

	keyboardMarkup := numericKeyboardClient
	if isAdmin {
		keyboardMarkup = numericKeyboard
	}
	t.SendMsgToTgbot(chatId, msg, keyboardMarkup)
}

func (t *Tgbot) SendMsgToTgbot(tgid int64, msg string, replyMarkup ...models.InlineKeyboardMarkup) {
	if !isRunning {
		return
	}

	if msg == "" {
		logger.Info("[tgbot] message is empty!")
		return
	}

	var allMessages []string
	limit := 2000

	// paging message if it is big
	if len(msg) > limit {
		messages := strings.Split(msg, "\r\n \r\n")
		lastIndex := -1

		for _, message := range messages {
			if (len(allMessages) == 0) || (len(allMessages[lastIndex])+len(message) > limit) {
				allMessages = append(allMessages, message)
				lastIndex++
			} else {
				allMessages[lastIndex] += "\r\n \r\n" + message
			}
		}
	} else {
		allMessages = append(allMessages, msg)
	}

	topicId := tgchat.TopicOf(adminChats, tgid)
	for _, message := range allMessages {
		params := &tgbotapi.SendMessageParams{
			ChatID:          tgid,
			MessageThreadID: topicId,
			Text:            message,
			ParseMode:       models.ParseModeHTML,
		}
		if len(replyMarkup) > 0 {
			params.ReplyMarkup = replyMarkup[0]
		}

		ctx, cancel := botCtx()
		_, err := bot.SendMessage(ctx, params)
		cancel()
		if err != nil {
			logger.Warning("Error sending telegram message :", err)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// sendFile uploads one local file to a chat, into its topic when it has one.
func sendFile(chatId int64, path string) error {
	handle, err := os.Open(path)
	if err != nil {
		return err
	}
	defer handle.Close()

	ctx, cancel := uploadCtx()
	defer cancel()
	_, err = bot.SendDocument(ctx, &tgbotapi.SendDocumentParams{
		ChatID:          chatId,
		MessageThreadID: tgchat.TopicOf(adminChats, chatId),
		Document: &models.InputFileUpload{
			Filename: filepath.Base(path),
			Data:     handle,
		},
	})
	return err
}

func (t *Tgbot) SendMsgToTgbotAdmins(msg string) {
	for _, chat := range adminChats {
		t.SendMsgToTgbot(chat.ChatID, msg)
	}
}

func (t *Tgbot) SendReport() {
	runTime, err := t.settingService.GetTgbotRuntime()
	if err == nil && len(runTime) > 0 {
		msg := ""
		msg += t.I18nBot("tgbot.messages.report", "RunTime=="+runTime)
		msg += t.I18nBot("tgbot.messages.datetime", "DateTime=="+time.Now().Format("2006-01-02 15:04:05"))
		t.SendMsgToTgbotAdmins(msg)
	}

	info := t.getServerUsage()
	t.SendMsgToTgbotAdmins(info)

	exhausted := t.getExhausted()
	t.SendMsgToTgbotAdmins(exhausted)

	backupEnable, err := t.settingService.GetTgBotBackup()
	if err == nil && backupEnable {
		t.SendBackupToAdmins()
	}
}

func (t *Tgbot) SendBackupToAdmins() {
	if !t.IsRunning() {
		return
	}
	for _, chat := range adminChats {
		t.sendBackup(chat.ChatID)
	}
}

func (t *Tgbot) getServerUsage() string {
	info, ipv4, ipv6 := "", "", ""
	info += t.I18nBot("tgbot.messages.hostname", "Hostname=="+hostname)
	info += t.I18nBot("tgbot.messages.version", "Version=="+config.GetVersion())

	// get ip address
	netInterfaces, err := net.Interfaces()
	if err != nil {
		logger.Error("net.Interfaces failed, err: ", err.Error())
		info += t.I18nBot("tgbot.messages.ip", "IP=="+t.I18nBot("tgbot.unknown"))
		info += " \r\n"
	} else {
		for i := 0; i < len(netInterfaces); i++ {
			if (netInterfaces[i].Flags & net.FlagUp) != 0 {
				addrs, _ := netInterfaces[i].Addrs()

				for _, address := range addrs {
					if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
						if ipnet.IP.To4() != nil {
							ipv4 += ipnet.IP.String() + " "
						} else if ipnet.IP.To16() != nil && !ipnet.IP.IsLinkLocalUnicast() {
							ipv6 += ipnet.IP.String() + " "
						}
					}
				}
			}
		}

		info += t.I18nBot("tgbot.messages.ipv4", "IPv4=="+ipv4)
		info += t.I18nBot("tgbot.messages.ipv6", "IPv6=="+ipv6)
	}

	// get latest status of server
	t.lastStatus = t.serverService.GetStatus(t.lastStatus)
	info += t.I18nBot("tgbot.messages.serverUpTime", "UpTime=="+strconv.FormatUint(t.lastStatus.Uptime/86400, 10), "Unit=="+t.I18nBot("tgbot.days"))
	info += t.I18nBot("tgbot.messages.serverLoad", "Load1=="+strconv.FormatFloat(t.lastStatus.Loads[0], 'f', 2, 64), "Load2=="+strconv.FormatFloat(t.lastStatus.Loads[1], 'f', 2, 64), "Load3=="+strconv.FormatFloat(t.lastStatus.Loads[2], 'f', 2, 64))
	info += t.I18nBot("tgbot.messages.serverMemory", "Current=="+common.FormatTraffic(int64(t.lastStatus.Mem.Current)), "Total=="+common.FormatTraffic(int64(t.lastStatus.Mem.Total)))
	info += t.I18nBot("tgbot.messages.tcpCount", "Count=="+strconv.Itoa(t.lastStatus.TcpCount))
	info += t.I18nBot("tgbot.messages.udpCount", "Count=="+strconv.Itoa(t.lastStatus.UdpCount))
	info += t.I18nBot("tgbot.messages.traffic", "Total=="+common.FormatTraffic(int64(t.lastStatus.NetTraffic.Sent+t.lastStatus.NetTraffic.Recv)), "Upload=="+common.FormatTraffic(int64(t.lastStatus.NetTraffic.Sent)), "Download=="+common.FormatTraffic(int64(t.lastStatus.NetTraffic.Recv)))
	info += t.I18nBot("tgbot.messages.xrayStatus", "State=="+fmt.Sprint(t.lastStatus.Xray.State))

	return info
}

func (t *Tgbot) UserLoginNotify(username string, ip string, time string, status LoginStatus) {
	if !t.IsRunning() {
		return
	}

	if username == "" || ip == "" || time == "" {
		logger.Warning("UserLoginNotify failed,invalid info")
		return
	}

	loginNotifyEnabled, err := t.settingService.GetTgBotLoginNotify()
	if err != nil || !loginNotifyEnabled {
		return
	}

	msg := ""
	switch status {
	case LoginSuccess:
		msg += t.I18nBot("tgbot.messages.loginSuccess")
	case LoginFail:
		msg += t.I18nBot("tgbot.messages.loginFailed")
	}
	msg += t.I18nBot("tgbot.messages.hostname", "Hostname=="+hostname)
	msg += t.I18nBot("tgbot.messages.username", "Username=="+username)
	msg += t.I18nBot("tgbot.messages.ip", "IP=="+ip)
	msg += t.I18nBot("tgbot.messages.time", "Time=="+time)

	t.SendMsgToTgbotAdmins(msg)
}

func (t *Tgbot) getInboundUsages() string {
	info := ""
	// get traffic
	inbounds, err := t.inboundService.GetAllInbounds()
	if err != nil {
		logger.Warning("GetAllInbounds run failed:", err)
		info += t.I18nBot("tgbot.answers.getInboundsFailed")
	} else {
		// NOTE:If there no any sessions here,need to notify here
		// TODO:Sub-node push, automatic conversion format
		for _, inbound := range inbounds {
			info += t.I18nBot("tgbot.messages.inbound", "Remark=="+inbound.Remark)
			info += t.I18nBot("tgbot.messages.port", "Port=="+strconv.Itoa(inbound.Port))
			info += t.I18nBot("tgbot.messages.traffic", "Total=="+common.FormatTraffic((inbound.Up+inbound.Down)), "Upload=="+common.FormatTraffic(inbound.Up), "Download=="+common.FormatTraffic(inbound.Down))

			if inbound.ExpiryTime == 0 {
				info += t.I18nBot("tgbot.messages.expire", "DateTime=="+t.I18nBot("tgbot.unlimited"))
			} else {
				info += t.I18nBot("tgbot.messages.expire", "DateTime=="+time.Unix((inbound.ExpiryTime/1000), 0).Format("2006-01-02 15:04:05"))
			}
		}
	}
	return info
}

func (t *Tgbot) clientInfoMsg(traffic *xray.ClientTraffic) string {
	expiryTime := ""
	if traffic.ExpiryTime == 0 {
		expiryTime = t.I18nBot("tgbot.unlimited")
	} else if traffic.ExpiryTime < 0 {
		expiryTime = fmt.Sprintf("%d %s", traffic.ExpiryTime/-86400000, t.I18nBot("tgbot.days"))
	} else {
		expiryTime = time.Unix((traffic.ExpiryTime / 1000), 0).Format("2006-01-02 15:04:05")
	}

	total := ""
	if traffic.Total == 0 {
		total = t.I18nBot("tgbot.unlimited")
	} else {
		total = common.FormatTraffic((traffic.Total))
	}

	active := ""
	if traffic.Enable {
		active = t.I18nBot("tgbot.messages.yes")
	} else {
		active = t.I18nBot("tgbot.messages.no")
	}

	status := "🔴 " + t.I18nBot("offline")
	if p.IsRunning() {
		for _, online := range GetOnlineUsersCache() {
			if online.Email == traffic.Email {
				status = "🟢 " + t.I18nBot("online")
				break
			}
		}
	}

	output := ""
	output += t.I18nBot("tgbot.messages.active", "Enable=="+active)
	output += t.I18nBot("tgbot.messages.online", "Status=="+status)
	output += t.I18nBot("tgbot.messages.email", "Email=="+traffic.Email)
	output += t.I18nBot("tgbot.messages.upload", "Upload=="+common.FormatTraffic(traffic.Up))
	output += t.I18nBot("tgbot.messages.download", "Download=="+common.FormatTraffic(traffic.Down))
	output += t.I18nBot("tgbot.messages.total", "UpDown=="+common.FormatTraffic((traffic.Up+traffic.Down)), "Total=="+total)
	output += t.I18nBot("tgbot.messages.expireIn", "Time=="+expiryTime)

	return output
}

func (t *Tgbot) getClientUsage(chatId int64, tgUserName string) {
	if len(tgUserName) == 0 {
		msg := t.I18nBot("tgbot.answers.askToAddUser")
		t.SendMsgToTgbot(chatId, msg)
		return
	}

	traffics, err := t.inboundService.GetClientTrafficTgBot(strconv.FormatInt(chatId, 10), tgUserName)
	if err != nil {
		logger.Warning(err)
		msg := t.I18nBot("tgbot.wentWrong")
		t.SendMsgToTgbot(chatId, msg)
		return
	}
	if len(traffics) == 0 {
		msg := t.I18nBot("tgbot.answers.askToAddUserName", "TgUserName=="+tgUserName)
		msg += "\r\n" + t.I18nBot("tgbot.commands.getID", "ID=="+strconv.FormatInt(chatId, 10))
		t.SendMsgToTgbot(chatId, msg)
		return
	}

	for _, traffic := range traffics {
		output := t.clientInfoMsg(traffic)
		t.SendMsgToTgbot(chatId, output)
	}
	t.SendAnswer(chatId, t.I18nBot("tgbot.commands.pleaseChoose"), false)
}

func (t *Tgbot) searchClient(chatId int64, email string) {
	traffic, err := t.inboundService.GetClientTrafficByEmail(email)
	if err != nil {
		logger.Warning(err)
		msg := t.I18nBot("tgbot.wentWrong")
		t.SendMsgToTgbot(chatId, msg)
		return
	}
	if traffic == nil {
		msg := t.I18nBot("tgbot.noResult")
		t.SendMsgToTgbot(chatId, msg)
		return
	}

	output := t.clientInfoMsg(traffic)
	t.SendMsgToTgbot(chatId, output)
}

func (t *Tgbot) searchInbound(chatId int64, remark string) {
	inbounds, err := t.inboundService.SearchInbounds(remark)
	if err != nil {
		logger.Warning(err)
		msg := t.I18nBot("tgbot.wentWrong")
		t.SendMsgToTgbot(chatId, msg)
		return
	}

	if len(inbounds) == 0 {
		msg := t.I18nBot("tgbot.noInbounds")
		t.SendMsgToTgbot(chatId, msg)
		return
	}

	for _, inbound := range inbounds {
		info := ""
		info += t.I18nBot("tgbot.messages.inbound", "Remark=="+inbound.Remark)
		info += t.I18nBot("tgbot.messages.port", "Port=="+strconv.Itoa(inbound.Port))
		info += t.I18nBot("tgbot.messages.traffic", "Total=="+common.FormatTraffic((inbound.Up+inbound.Down)), "Upload=="+common.FormatTraffic(inbound.Up), "Download=="+common.FormatTraffic(inbound.Down))

		if inbound.ExpiryTime == 0 {
			info += t.I18nBot("tgbot.messages.expire", "DateTime=="+t.I18nBot("tgbot.unlimited"))
		} else {
			info += t.I18nBot("tgbot.messages.expire", "DateTime=="+time.Unix((inbound.ExpiryTime/1000), 0).Format("2006-01-02 15:04:05"))
		}
		t.SendMsgToTgbot(chatId, info)

		for _, traffic := range inbound.ClientStats {
			output := t.clientInfoMsg(&traffic)
			t.SendMsgToTgbot(chatId, output)
		}
	}
}

func (t *Tgbot) searchForClient(chatId int64, query string) {
	traffic, err := t.inboundService.SearchClientTraffic(query)
	if err != nil {
		logger.Warning(err)
		msg := t.I18nBot("tgbot.wentWrong")
		t.SendMsgToTgbot(chatId, msg)
		return
	}
	if traffic == nil {
		msg := t.I18nBot("tgbot.noResult")
		t.SendMsgToTgbot(chatId, msg)
		return
	}

	output := t.clientInfoMsg(traffic)
	t.SendMsgToTgbot(chatId, output)
}

func (t *Tgbot) getExhausted() string {
	trDiff := int64(0)
	exDiff := int64(0)
	now := time.Now().Unix() * 1000
	var exhaustedInbounds []model.Inbound
	var exhaustedClients []xray.ClientTraffic
	var disabledInbounds []model.Inbound
	var disabledClients []xray.ClientTraffic

	TrafficThreshold, err := t.settingService.GetTrafficDiff()
	if err == nil && TrafficThreshold > 0 {
		trDiff = int64(TrafficThreshold) * 1073741824
	}
	ExpireThreshold, err := t.settingService.GetExpireDiff()
	if err == nil && ExpireThreshold > 0 {
		exDiff = int64(ExpireThreshold) * 86400000
	}
	inbounds, err := t.inboundService.GetAllInbounds()
	if err != nil {
		logger.Warning("Unable to load Inbounds", err)
	}

	for _, inbound := range inbounds {
		if inbound.Enable {
			if (inbound.ExpiryTime > 0 && (inbound.ExpiryTime-now < exDiff)) ||
				(inbound.Total > 0 && (inbound.Total-(inbound.Up+inbound.Down) < trDiff)) {
				exhaustedInbounds = append(exhaustedInbounds, *inbound)
			}
			if len(inbound.ClientStats) > 0 {
				for _, client := range inbound.ClientStats {
					if client.Enable {
						if (client.ExpiryTime > 0 && (client.ExpiryTime-now < exDiff)) ||
							(client.Total > 0 && (client.Total-(client.Up+client.Down) < trDiff)) {
							exhaustedClients = append(exhaustedClients, client)
						}
					} else {
						disabledClients = append(disabledClients, client)
					}
				}
			}
		} else {
			disabledInbounds = append(disabledInbounds, *inbound)
		}
	}

	// Inbounds
	output := ""
	output += t.I18nBot("tgbot.messages.exhaustedCount", "Type=="+t.I18nBot("tgbot.inbounds"))
	output += t.I18nBot("tgbot.messages.disabled", "Disabled=="+strconv.Itoa(len(disabledInbounds)))
	output += t.I18nBot("tgbot.messages.depleteSoon", "Deplete=="+strconv.Itoa(len(exhaustedInbounds)))
	output += "\r\n \r\n"

	if len(exhaustedInbounds) > 0 {
		output += t.I18nBot("tgbot.messages.exhaustedMsg", "Type=="+t.I18nBot("tgbot.inbounds"))

		for _, inbound := range exhaustedInbounds {
			output += t.I18nBot("tgbot.messages.inbound", "Remark=="+inbound.Remark)
			output += t.I18nBot("tgbot.messages.port", "Port=="+strconv.Itoa(inbound.Port))
			output += t.I18nBot("tgbot.messages.traffic", "Total=="+common.FormatTraffic((inbound.Up+inbound.Down)), "Upload=="+common.FormatTraffic(inbound.Up), "Download=="+common.FormatTraffic(inbound.Down))
			if inbound.ExpiryTime == 0 {
				output += t.I18nBot("tgbot.messages.expire", "DateTime=="+t.I18nBot("tgbot.unlimited"))
			} else {
				output += t.I18nBot("tgbot.messages.expire", "DateTime=="+time.Unix((inbound.ExpiryTime/1000), 0).Format("2006-01-02 15:04:05"))
			}
			output += "\r\n \r\n"
		}
	}

	// Clients
	output += t.I18nBot("tgbot.messages.exhaustedCount", "Type=="+t.I18nBot("tgbot.clients"))
	output += t.I18nBot("tgbot.messages.disabled", "Disabled=="+strconv.Itoa(len(disabledClients)))
	output += t.I18nBot("tgbot.messages.depleteSoon", "Deplete=="+strconv.Itoa(len(exhaustedClients)))
	output += "\r\n \r\n"

	if len(exhaustedClients) > 0 {
		output += t.I18nBot("tgbot.messages.exhaustedMsg", "Type=="+t.I18nBot("tgbot.clients"))

		for _, traffic := range exhaustedClients {
			output += t.clientInfoMsg(&traffic)
			output += "\r\n \r\n"
		}
	}

	return output
}

func (t *Tgbot) onlineClients(chatId int64) {
	if !p.IsRunning() {
		return
	}

	onlines := GetOnlineUsersCache()
	output := t.I18nBot("tgbot.messages.onlinesCount", "Count=="+fmt.Sprint(len(onlines)))
	if len(onlines) > 0 {
		keyboard := models.InlineKeyboardMarkup{}
		for index, online := range onlines {
			keyboard.InlineKeyboard = append(keyboard.InlineKeyboard, []models.InlineKeyboardButton{
				{Text: fmt.Sprintf("%d: %s\r\n", index+1, online.Email), CallbackData: "client_" + online.Email},
			})
		}
		t.SendMsgToTgbot(chatId, output, keyboard)
	} else {
		t.SendMsgToTgbot(chatId, output)
	}
}

func (t *Tgbot) sendBackup(chatId int64) {
	if !t.IsRunning() {
		return
	}

	// Update by manually trigger a checkpoint operation
	err := database.Checkpoint()
	if err != nil {
		logger.Warning("Error in trigger a checkpoint operation: ", err)
	}

	output := t.I18nBot("tgbot.messages.backupTime", "Time=="+time.Now().Format("2006-01-02 15:04:05"))
	t.SendMsgToTgbot(chatId, output)

	if err = sendFile(chatId, config.GetDBPath()); err != nil {
		logger.Warning("Error in uploading backup: ", err)
	}

	if err = sendFile(chatId, xray.GetConfigPath()); err != nil {
		logger.Warning("Error in uploading config.json: ", err)
	}
}
