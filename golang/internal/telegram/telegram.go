// Package telegram carries the session's messages to the people watching it.
//
// It is a one-way channel today: the agent posts, the chat reads. Inbound
// messages need a live session to deliver them into, which arrives with the
// harness; until then the bot polls nothing, because a reader nobody consumes is
// a leak dressed as a feature.
package telegram

import (
	"context"
	"fmt"

	"github.com/mymmrac/telego"
	telegoapi "github.com/mymmrac/telego/telegoapi"
	"go.uber.org/zap"
)

// Config is what the channel needs to exist. An empty Token means the operator
// did not configure a channel at all, which is a legal state: the caller then
// offers the agent no way to post rather than failing to send.
type Config struct {
	Token   string
	ChatID  int64
	TopicID int
	// APIServer overrides Telegram's own address. Empty means the real one.
	APIServer string
}

func (c Config) Configured() bool { return c.Token != "" && c.ChatID != 0 }

type Bot struct {
	bot     *telego.Bot
	chatID  int64
	topicID int
	log     *zap.Logger
}

func New(cfg Config, log *zap.Logger) (*Bot, error) {
	if !cfg.Configured() {
		return nil, fmt.Errorf("telegram needs TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID")
	}

	// net/http rather than the default caller: it honours HTTP_PROXY, and the
	// network this runs on has no other way out.
	options := []telego.BotOption{telego.WithAPICaller(telegoapi.DefaultHTTPCaller)}
	if cfg.APIServer != "" {
		options = append(options, telego.WithAPIServer(cfg.APIServer))
	}

	bot, err := telego.NewBot(cfg.Token, options...)
	if err != nil {
		return nil, fmt.Errorf("create telegram bot: %w", err)
	}

	return &Bot{bot: bot, chatID: cfg.ChatID, topicID: cfg.TopicID, log: log}, nil
}

// Send posts one message and returns the id Telegram gave it. A configured topic
// is always passed: without it the message lands in the group's General tab
// instead of the thread the team is reading.
func (b *Bot) Send(ctx context.Context, text string) (int, error) {
	if text == "" {
		return 0, fmt.Errorf("refusing to send an empty message")
	}

	params := &telego.SendMessageParams{
		ChatID: telego.ChatID{ID: b.chatID},
		Text:   text,
	}
	if b.topicID != 0 {
		params.MessageThreadID = b.topicID
	}

	sent, err := b.bot.SendMessage(ctx, params)
	if err != nil {
		return 0, fmt.Errorf("send telegram message: %w", err)
	}
	b.log.Info("posted to telegram", zap.Int("message_id", sent.MessageID))

	return sent.MessageID, nil
}
