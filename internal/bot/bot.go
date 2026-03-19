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
	b.sendMenu(msg.Chat.ID, tgID, mainMenu(), 0)
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
		b.sendVPNKey(chatID, cb.From.ID, keyMenu(), messageID)
	case "menu:profile":
		b.sendProfile(chatID, cb.From.ID, profileMenu(), messageID)
	case "menu:buy":
		buyMarkup := buyMenu()
		b.editMessage(chatID, messageID, "Выберите способ оплаты:", &buyMarkup)
	case "menu:help":
		b.editMessage(chatID, messageID, "Доступные команды:\n/start\n/profile", nil)
	case "menu:reset_key":
		b.sendResetKey(chatID, cb.From.ID, keyMenu(), messageID)
	case "profile:extend":
		extendMarkup := buyMenu()
		b.editMessage(chatID, messageID, "Выберите способ продления:", &extendMarkup)
	case "menu:main":
		b.sendMenu(chatID, cb.From.ID, mainMenu(), messageID)
	case "key:help":
		b.sendInstruction(chatID, cb.From.ID, instructionMenu(), messageID)
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

func (b *Bot) sendVPNKey(chatID int64, tgID int64, markup tgbotapi.InlineKeyboardMarkup, messageID int) {
	ctx := context.Background()
	key, err := b.userSrv.GenerateVPNKey(ctx, tgID)
	if err != nil {
		b.reply(chatID, "Пользователь не найден.")
		return
	}
	text := fmt.Sprintf("🔑 Ваш VPN ключ (нажмите, чтобы скопировать):\n\n"+"<code>%s</code>"+"\n\n"+
		"Рекомендуемое приложение: \n\n"+
		"📱 iOS — Happ\n"+
		"📱 Android — v2RayTun\n"+
		"💻 ПК — Happ", key)
	if messageID == 0 {
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = tgbotapi.ModeHTML
		msg.ReplyMarkup = markup
		if _, err := b.api.Send(msg); err != nil {
			log.Println("Ошибка отправки сообщения:", err)
		}
		return
	}

	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	edit.ParseMode = tgbotapi.ModeHTML
	edit.ReplyMarkup = &markup
	if _, err := b.api.Request(edit); err != nil {
		log.Println("failed to edit message:", err)
	}
}

func (b *Bot) sendResetKey(chatID int64, tgID int64, markup tgbotapi.InlineKeyboardMarkup, messageID int) {
	ctx := context.Background()
	key, err := b.userSrv.GenerateVPNKey(ctx, tgID)
	if err != nil {
		b.reply(chatID, "Пользователь не найден.")
		return
	}
	text := fmt.Sprintf("✅ VPN ключ успешно обновлен\n\n"+
		"🔑 Ваш VPN ключ (нажмите, чтобы скопировать):\n\n"+"<code>%s</code>"+"\n\n"+
		"Рекомендуемое приложение: \n\n"+
		"📱 iOS — Happ\n"+
		"📱 Android — v2RayTun\n"+
		"💻 ПК — Happ", key)
	if messageID == 0 {
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = tgbotapi.ModeHTML
		msg.ReplyMarkup = markup
		if _, err := b.api.Send(msg); err != nil {
			log.Println("Ошибка отправки сообщения:", err)
		}
		return
	}

	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	edit.ParseMode = tgbotapi.ModeHTML
	edit.ReplyMarkup = &markup
	if _, err := b.api.Request(edit); err != nil {
		log.Println("failed to edit message:", err)
	}
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

	text := fmt.Sprintf(
		"👤 <strong>Профиль</strong>\n\n"+
			"🆔 <strong>Идентификатор</strong>: %d\n"+
			"💰 <strong>Баланс</strong>: %d ₽\n"+
			"📅 <strong>Дата окончания</strong>:\n%v\n"+
			"Осталось: %d дней",
		user.TelegramID,
		user.Balance,
		subText,
		days+1,
	)

	if messageID == 0 {
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = tgbotapi.ModeHTML
		msg.ReplyMarkup = markup
		if _, err := b.api.Send(msg); err != nil {
			log.Println("Ошибка отправки сообщения:", err)
		}
		return
	}

	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	edit.ParseMode = tgbotapi.ModeHTML
	edit.ReplyMarkup = &markup
	if _, err := b.api.Request(edit); err != nil {
		log.Println("failed to edit message:", err)
	}
}

func (b *Bot) sendMenu(chatID int64, tgID int64, markup tgbotapi.InlineKeyboardMarkup, messageID int) {
	ctx := context.Background()
	user, err := b.userSrv.GetUser(ctx, tgID)
	if err != nil || user == nil {
		user, err = b.userSrv.RegisterUser(ctx, tgID)
		if err != nil {
			b.reply(chatID, "Ошибка регистрации пользователя.")
		return
		}
	}

	var text string

	if user.Status == "active" {
		subTime := time.Unix(user.SubUntil, 0)
		subText := subTime.Format("02.01.2006 15:04")
		text = fmt.Sprintf("🚀 <strong>SunaVPN</strong>\n\n"+
			"<strong>Статус</strong>: ✅ Активна\n"+
			"<strong>Действует до</strong>: %v\n\n"+
			"👇 Выберите действие",
			subText)
	} else {
		text = "🚀 <strong>SunaVPN</strong>\n\n" +
			"<strong>Статус</strong>: ❌ Не активна\n\n" +
			"👇 Выберите действие"
	}

	if messageID == 0 {
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = tgbotapi.ModeHTML
		msg.ReplyMarkup = markup
		if _, err := b.api.Send(msg); err != nil {
			log.Println("Ошибка отправки сообщения:", err)
		}
		return
	}

	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	edit.ParseMode = tgbotapi.ModeHTML
	edit.ReplyMarkup = &markup
	if _, err := b.api.Request(edit); err != nil {
		log.Println("failed to edit message:", err)
	}
}

func (b *Bot) sendInstruction(chatID int64, tgID int64, markup tgbotapi.InlineKeyboardMarkup, messageID int) {
	ctx := context.Background()
	user, err := b.userSrv.GetUser(ctx, tgID)
	if err != nil || user == nil {
		b.reply(chatID, "Пользователь не найден.")
		return
	}

	var text string

	text = "🚀 Добро пожаловать в <strong>SunaVPN</strong>\n\n" +
		"Чтобы подключиться к VPN, выполните следующие шаги:\n\n" +
		"1️⃣ <strong>Скопируйте свой VPN ключ</strong>\n" +
		"- Нажмите на 🔁 'Обновить ключ' или просто скопируйте текст ключа.\n\n" +
		"2️⃣ <strong>Выберите приложение для подключения</strong>\n" +
		"- 📱 iOS — Happ\n" +
		"- 📱 Android — v2RayTun\n" +
		"- 💻 ПК — Happ\n\n" +
		"3️⃣ <strong>Вставьте ключ в приложение</strong>\n" +
		"- В Happ или v2RayTun выберите 'Импорт из текста' и вставьте скопированный ключ.\n" +
		"- В Happ для ПК выберите 'Добавить профиль' → 'Импорт из текста' и вставьте ключ.\n" +
		"- Настройки по умолчанию уже подходят для подключения.\n\n" +
		"4️⃣ <strong>Подключитесь</strong>\n" +
		"- Нажмите 'Подключить' и убедитесь, что интернет работает через VPN.\n" +
		"- Если соединение не устанавливается, попробуйте обновить ключ (🔑✨ Новый ключ).\n\n" +
		"❗ <strong>Совет:</strong>\n" +
		"- Используйте ключ только на своих устройствах (максимум 5 устройств на один аккаунт).\n" +
		"- Не передавайте ключ другим людям.\n\n"

	if messageID == 0 {
		msg := tgbotapi.NewMessage(chatID, text)
		msg.ParseMode = tgbotapi.ModeHTML
		msg.ReplyMarkup = markup
		if _, err := b.api.Send(msg); err != nil {
			log.Println("Ошибка отправки сообщения:", err)
		}
		return
	}

	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	edit.ParseMode = tgbotapi.ModeHTML
	edit.ReplyMarkup = &markup
	if _, err := b.api.Request(edit); err != nil {
		log.Println("failed to edit message:", err)
	}
}

// utility keyboard builders
func mainMenu() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("👤 Профиль", "menu:profile"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("💰 Пополнить баланс", "menu:buy"),
			tgbotapi.NewInlineKeyboardButtonData("🔑 Получить ключ", "menu:get_key"),
		),
	)
}

func keyMenu() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📖 Инструкция", "key:help"),
			tgbotapi.NewInlineKeyboardButtonData("🔁 Обновить ключ", "menu:reset_key"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", "menu:main"),
		),
	)
}

func instructionMenu() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔁 Обновить ключ", "menu:reset_key"),
			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", "menu:get_key"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🏠 Главное меню", "menu:main"),
		),
	)
}

func profileMenu() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔑 Получить ключ", "menu:get_key"),
			tgbotapi.NewInlineKeyboardButtonData("💰 Пополнить баланс", "profile:extend"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔙 Назад", "menu:main"),
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
