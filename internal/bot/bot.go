package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"vpn-bot/internal/service"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Bot struct {
	api     *tgbotapi.BotAPI
	userSrv *service.UserService
}

func New(token string, userSrv *service.UserService) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, err
	}

	return &Bot{api: api, userSrv: userSrv}, nil
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
	ctx := context.Background()

	user, isNew, err := b.userSrv.RegisterUser(ctx, tgID)
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

	key, err := b.userSrv.GenerateVPNKey(ctx, tgID)
	if err != nil {
		log.Println("failed to generate vpn key:", err)
		b.reply(msg.Chat.ID, "Ошибка при получении VPN ключа.")
		return
	}

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
	if _, err := b.api.Send(msgOut); err != nil {
		log.Println("Ошибка отправки сообщения:", err)
	}
}

func (b *Bot) handleProfile(msg *tgbotapi.Message) {
	tgID := msg.From.ID
	b.sendProfile(msg.Chat.ID, tgID, profileMenu(), 0)
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
	if cb == nil {
		return
	}

	// answer to remove "loading" indicator (Telegram client needs it)
	if _, err := b.api.Request(tgbotapi.NewCallback(cb.ID, "")); err != nil {
		log.Println("failed to answer callback:", err)
	}

	chatID := cb.From.ID
	messageID := 0
	if cb.Message != nil {
		chatID = cb.Message.Chat.ID
		messageID = cb.Message.MessageID
	}
	log.Printf("callback received: data=%q from=%d chat=%d msg=%d", cb.Data, cb.From.ID, chatID, messageID)

	switch cb.Data {
	case "menu:get_key":
		b.sendVPNKey(chatID, cb.From.ID)
	case "menu:profile":
		b.sendProfile(chatID, cb.From.ID, profileMenu(), messageID)
	case "menu:buy":
		buyMarkup := buyMenu()
		b.editMessage(chatID, messageID, "Выберите способ оплаты:", &buyMarkup)
	case "menu:help":
		b.editMessage(chatID, messageID, "Доступные команды:\n/start\n/profile", nil)
	case "profile:show_key":
		b.sendVPNKey(chatID, cb.From.ID)
	case "profile:extend":
		extendMarkup := buyMenu()
		b.editMessage(chatID, messageID, "Выберите способ продления:", &extendMarkup)
	case "menu:main":
		mainMarkup := mainMenu()
		b.editMessage(chatID, messageID, "Главное меню", &mainMarkup)
	case "buy:card":
		if err := b.userSrv.StartPayment(context.Background(), cb.From.ID, "card"); err != nil {
			log.Println("failed to start payment flow:", err)
			b.reply(chatID, "Не удалось начать оплату. Попробуйте позже.")
			return
		}
		b.editMessage(chatID, messageID, "Введите сумму в рублях для оплаты картой:", nil)
	case "buy:telegram":
		if err := b.userSrv.StartPayment(context.Background(), cb.From.ID, "telegram"); err != nil {
			log.Println("failed to start payment flow:", err)
			b.reply(chatID, "Не удалось начать оплату. Попробуйте позже.")
			return
		}
		b.editMessage(chatID, messageID, "Введите сумму в рублях для оплаты через Telegram:", nil)
	default:
		log.Printf("unknown callback data: %q", cb.Data)
	}
}

func (b *Bot) editMessage(chatID int64, messageID int, text string, markup *tgbotapi.InlineKeyboardMarkup) {
	if messageID == 0 {
		// fallback to sending a new message if we don't have a message ID
		b.sendMessage(chatID, text, markup)
		return
	}

	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	edit.ReplyMarkup = markup
	if _, err := b.api.Request(edit); err != nil {
		log.Println("failed to edit message:", err)
	}
}

func (b *Bot) sendMessage(chatID int64, text string, markup interface{}) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ReplyMarkup = markup
	if _, err := b.api.Send(msg); err != nil {
		log.Println("Ошибка при отправке сообщения:", err)
	}
}

func (b *Bot) handlePendingPurchase(msg *tgbotapi.Message) bool {
	// Preserve command handling even if a pending payment exists.
	if msg.IsCommand() {
		return false
	}

	ctx := context.Background()
	pending, err := b.userSrv.GetPendingPayment(ctx, msg.From.ID)
	if err != nil {
		log.Println("failed to fetch pending payment:", err)
		return false
	}
	if pending == nil {
		return false
	}

	amount, err := strconv.Atoi(strings.TrimSpace(msg.Text))
	if err != nil || amount <= 0 {
		b.reply(msg.Chat.ID, "Некорректная сумма. Попробуйте ещё раз.")
		return true
	}

	user, err := b.userSrv.CompletePayment(ctx, msg.From.ID, amount)
	if err != nil {
		log.Println("payment processing error:", err)
		b.reply(msg.Chat.ID, "Ошибка при обработке платежа. Попробуйте позже.")
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
	ctx := context.Background()
	key, err := b.userSrv.GenerateVPNKey(ctx, tgID)
	if err != nil {
		b.reply(chatID, "Пользователь не найден.")
		return
	}
	b.reply(chatID, "Ваш VPN ключ:\n"+key)
}

func (b *Bot) sendProfile(chatID int64, tgID int64, markup tgbotapi.InlineKeyboardMarkup, messageID int) {
	ctx := context.Background()
	user, err := b.userSrv.GetUser(ctx, tgID)
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

	key, err := b.userSrv.GenerateVPNKey(ctx, tgID)
	if err != nil {
		b.reply(chatID, "Не удалось получить VPN ключ.")
		return
	}
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

	if messageID == 0 {
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ReplyMarkup = markup
		if _, err := b.api.Send(msg); err != nil {
			log.Println("Ошибка отправки сообщения:", err)
		}
		return
	}

	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	edit.ReplyMarkup = &markup
	if _, err := b.api.Request(edit); err != nil {
		log.Println("failed to edit message:", err)
	}
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
