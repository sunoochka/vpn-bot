package bot

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
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
	// xray manager may still be kept for future admin commands but the
	// service already handles adding users so handlers shouldn't call it
	// directly.
	xray xray.ManagerInterface

	// simple in-memory state for users who are currently entering a
	// payment amount. We key by chat ID and store the payment method
	// requested ("card" or "telegram"). A mutex guards concurrent
	// access since updates arrive on the bot goroutine.
	purchaseState map[int64]string
	psMu          sync.Mutex
}

func New(token string, userSrv *service.UserService, vpnCfg vpn.Config, xrayMgr xray.ManagerInterface) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, err
	}

	return &Bot{
		api:           api,
		userSrv:       userSrv,
		vpnConfig:     vpnCfg,
		xray:          xrayMgr,
		purchaseState: make(map[int64]string),
	}, nil
}

func (b *Bot) Start() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	for update := range updates {
		if update.CallbackQuery != nil {
			b.handleCallback(update.CallbackQuery)
			continue
		}
		if update.Message == nil {
			continue
		}

		// check whether the user was entering a payment amount
		if b.handlePendingPurchase(update.Message) {
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
		text = "🎉 Добро пожаловать!\n\n" +
			"Вам выдано 3 дня бесплатного VPN.\n\n" +
			"Ваш ключ:\n\n" + key
	} else {
		text = "👋 С возвращением!\n\n" +
			"🔑 Ваш VPN ключ:\n\n" + key
	}

	msgOut := tgbotapi.NewMessage(msg.Chat.ID, text)
	msgOut.ReplyMarkup = mainMenu()
	b.api.Send(msgOut)
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

	key := vpn.GenerateKey(user.UUID, b.vpnConfig)

	text := fmt.Sprintf(
		"👤 Ваш профиль\n\n"+
		"🆔Идентификатор: %d\n"+
		"💰Баланс: %d ₽\n"+
		"📲Устройств: %d\n\n"+
		"📅Дата окончания:\n%v\n"+
		"Осталось: %d дней\n\n"+
		"🔑Ваш VPN ключ:\n%s",
		user.TelegramID,
		user.Balance,
		user.Devices,
		subText,
		days+1,
		key,
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

// ---- callback and payment-related helpers ----

func (b *Bot) handleCallback(cb *tgbotapi.CallbackQuery) {
	data := cb.Data
	// answer to remove "loading" indicator
	answer := tgbotapi.NewCallback(cb.ID, "")
	if _, err := b.api.Request(answer); err != nil {
		log.Println("failed to answer callback:", err)
	}

	switch data {
	case "menu:get_key":
		b.sendVPNKey(cb.Message.Chat.ID, cb.From.ID)
	case "menu:profile":
		b.sendProfile(cb.Message.Chat.ID, cb.From.ID, profileMenu())
	case "menu:buy":
		msg := tgbotapi.NewMessage(cb.Message.Chat.ID, "Выберите способ оплаты:")
		msg.ReplyMarkup = buyMenu()
		b.api.Send(msg)
	case "menu:help":
		b.api.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, "Доступные команды:\n/start\n/profile"))
	case "profile:show_key":
		b.sendVPNKey(cb.Message.Chat.ID, cb.From.ID)
	case "profile:extend":
		msg := tgbotapi.NewMessage(cb.Message.Chat.ID, "Выберите способ продления:")
		msg.ReplyMarkup = buyMenu()
		b.api.Send(msg)
	case "menu:main":
		msg := tgbotapi.NewMessage(cb.Message.Chat.ID, "Главное меню")
		msg.ReplyMarkup = mainMenu()
		b.api.Send(msg)
	case "buy:card":
		b.setPurchaseState(cb.Message.Chat.ID, "card")
		b.api.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, "Введите сумму в рублях для оплаты картой:"))
	case "buy:telegram":
		b.setPurchaseState(cb.Message.Chat.ID, "telegram")
		b.api.Send(tgbotapi.NewMessage(cb.Message.Chat.ID, "Введите сумму в рублях для оплаты через Telegram:"))
	default:
		// unknown callback ignored
	}
}

func (b *Bot) setPurchaseState(chatID int64, method string) {
	b.psMu.Lock()
	defer b.psMu.Unlock()
	b.purchaseState[chatID] = method
}

func (b *Bot) clearPurchaseState(chatID int64) {
	b.psMu.Lock()
	defer b.psMu.Unlock()
	delete(b.purchaseState, chatID)
}

func (b *Bot) getPurchaseState(chatID int64) (string, bool) {
	b.psMu.Lock()
	defer b.psMu.Unlock()
	m, ok := b.purchaseState[chatID]
	return m, ok
}

func (b *Bot) handlePendingPurchase(msg *tgbotapi.Message) bool {
	_, ok := b.getPurchaseState(msg.Chat.ID)
	if !ok {
		return false
	}
	b.clearPurchaseState(msg.Chat.ID)

	amount, err := strconv.Atoi(strings.TrimSpace(msg.Text))
	if err != nil || amount <= 0 {
		b.reply(msg.Chat.ID, "Некорректная сумма. Попробуйте ещё раз.")
		return true
	}

	user, err := b.userSrv.ProcessPayment(msg.From.ID, amount)
	if err != nil {
		log.Println("payment processing error:", err)
		b.reply(msg.Chat.ID, "Ошибка при обработке платежа.")
		return true
	}

	subText := ""
	if user.SubUntil > 0 {
		subText = time.Unix(user.SubUntil, 0).Format("02.01.2006 15:04")
	}

	b.reply(msg.Chat.ID, fmt.Sprintf("Платеж на %d ₽ принят. Баланс: %d ₽. Подписка до: %s", amount, user.Balance, subText))
	return true
}

func (b *Bot) sendVPNKey(chatID int64, tgID int64) {
	user, err := b.userSrv.GetUser(tgID)
	if err != nil || user == nil {
		b.reply(chatID, "Пользователь не найден.")
		return
	}
	key := vpn.GenerateKey(user.UUID, b.vpnConfig)
	b.reply(chatID, "Ваш VPN ключ:\n"+key)
}

func (b *Bot) sendProfile(chatID int64, tgID int64, markup tgbotapi.InlineKeyboardMarkup) {
	user, err := b.userSrv.GetUser(tgID)
	if err != nil || user == nil {
		b.reply(chatID, "Пользователь не найден.")
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
	key := vpn.GenerateKey(user.UUID, b.vpnConfig)
	text := fmt.Sprintf(
		"👤 Ваш профиль\n\n"+
		"🆔Идентификатор: %d\n"+
		"💰Баланс: %d ₽\n"+
		"📲Устройств: %d\n\n"+
		"📅Дата окончания:\n%v\n"+
		"Осталось: %d дней\n\n"+
		"🔑Ваш VPN ключ:\n%s",
		user.TelegramID,
		user.Balance,
		user.Devices,
		subText,
		days+1,
		key,
	)
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = markup
	b.api.Send(msg)
}

// utility keyboard builders
func mainMenu() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Get VPN Key", "menu:get_key"),
			tgbotapi.NewInlineKeyboardButtonData("Profile", "menu:profile"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Buy Subscription", "menu:buy"),
			tgbotapi.NewInlineKeyboardButtonData("Help", "menu:help"),
		),
	)
}

func profileMenu() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Show VPN Key", "profile:show_key"),
			tgbotapi.NewInlineKeyboardButtonData("Extend Subscription", "profile:extend"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Back", "menu:main"),
		),
	)
}

func buyMenu() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Bank Card", "buy:card"),
			tgbotapi.NewInlineKeyboardButtonData("Telegram Pay", "buy:telegram"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Back", "menu:main"),
		),
	)
}

