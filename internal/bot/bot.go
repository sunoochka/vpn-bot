package bot

import (
	"fmt"
	"log"
	"time"
	"vpn-bot/internal/service"
	"vpn-bot/internal/vpn"
	"vpn-bot/internal/xray"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Bot struct {
	api       *tgbotapi.BotAPI
	userSrv   *service.UserService
	vpnConfig vpn.Config
	xray *xray.Manager
}

func New(token string, userSrv *service.UserService, vpnCfg vpn.Config) (*Bot, error) {
	xrayManager := xray.NewManager("/usr/local/etc/xray/config.json")

	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, err
	}

	return &Bot{
		api:       api,
		userSrv:   userSrv,
		vpnConfig: vpnCfg,
		xray: xrayManager,
	}, nil
}

func (b *Bot) Start() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil {
			continue
		}
		switch update.Message.Command() {
		case "start":
			b.handleStart(update.Message)
		case "profile":
			b.handleProfile(update.Message)
		default:
			b.handleUnknown(update.Message)
		}
	}
}

func (b *Bot) handleStart(msg *tgbotapi.Message) {
	tgID := msg.From.ID

	user, isNew, err := b.userSrv.RegisterUser(tgID)
	if err != nil {
		log.Println(err)
		b.reply(msg.Chat.ID, "Ошибка при получении данных пользователя.")
		return
	}

	if user == nil {
		log.Println("RegisterUser returned nil user")
		b.reply(msg.Chat.ID, "Ошибка регистрации пользователя.")
		return
	}

	key := vpn.GenerateKey(user.UUID, b.vpnConfig)

	var text string

	if isNew {
		err := b.xray.AddUser(user.UUID)
		if err != nil {
			log.Println("Ошибка при добавлении пользователя в Xray:", err)
			b.reply(msg.Chat.ID, "Ошибка при настройке VPN. Пожалуйста, попробуйте позже.")
			return
		}

		text = "🎉 Добро пожаловать!\n\n" +
			"Вам выдано 3 дня бесплатного VPN.\n\n" +
			"Ваш ключ:\n\n" + key
	} else {
		text = "👋 С возвращением!\n\n" +
			"🔑 Ваш VPN ключ:\n\n" + key
	}

	b.reply(msg.Chat.ID, text)
}

func (b *Bot) handleProfile(msg *tgbotapi.Message) {
	tgID := msg.From.ID

	user, err := b.userSrv.GetUser(tgID)
	if err != nil {
		log.Println(err)
		return
	}

	if user == nil {
		b.reply(msg.Chat.ID, "Пользователь не найден. Пожалуйста, используйте /start для регистрации.")
		return
	}

	var subText string
	var days int

	if user.SubUntil == 0 {
		subText = "Нет активной подписки"
	} else {
		subTime := time.Unix(user.SubUntil, 0)
		subText = subTime.Format("02.01.2006 15:04")
		days = int(time.Until(subTime).Hours() / 24)
		if days < 0 {
			days = 0
		}
	}

	text := fmt.Sprintf(
		"👤 Ваш профиль\n\n"+
			"🆔Идентификатор: %d\n"+
			"💰Баланс: %d ₽\n"+
			"📲Устройств: %d\n\n"+
			"📅Дата окончания:\n%v\n"+
			"Осталось: %d дней",
		user.TelegramID,
		user.Balance,
		user.Devices,
		subText,
		days+1,
	)
	b.reply(msg.Chat.ID, text)
}

func (b *Bot) handleUnknown(msg *tgbotapi.Message) {
	b.reply(msg.Chat.ID, "Неизвестная команда. Пожалуйста, используйте /start для получения VPN ключа.")
}

func (b *Bot) reply(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	_, err := b.api.Send(msg)
	if err != nil {
		log.Println("Ошибка при отправке сообщения:", err)
	}
}
