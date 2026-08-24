// Package telegram is the room the session works in: everything it says is
// posted there, and what a person writes back reaches the session.
//
// The agent knows nothing about any of this. The harness reads the session's
// output and posts it; the harness reads the chat and gives it to the session.
// Keeping the channel out of the agent's tools is what makes a session started
// by the clock and a session started by a person the same thing.
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
	// AllowUserIDs are the people whose messages reach the session. Empty means
	// nobody: an open channel into a trading agent is not a default.
	AllowUserIDs []int64
	// APIServer overrides Telegram's own address. Empty means the real one.
	APIServer string
}

func (c Config) Configured() bool { return c.Token != "" && c.ChatID != 0 }

// Message is what a person wrote in the chat, after the allowlist.
type Message struct {
	Text     string
	UserID   int64
	Username string
}

type Bot struct {
	bot     *telego.Bot
	chatID  int64
	topicID int
	allowed map[int64]bool
	log     *zap.Logger

	inbound chan Message
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

	allowed := make(map[int64]bool, len(cfg.AllowUserIDs))
	for _, id := range cfg.AllowUserIDs {
		allowed[id] = true
	}

	return &Bot{
		bot:     bot,
		chatID:  cfg.ChatID,
		topicID: cfg.TopicID,
		allowed: allowed,
		log:     log,
		inbound: make(chan Message, inboundBuffer),
	}, nil
}

// Inbound is the stream of messages from people allowed to talk to the session.
func (b *Bot) Inbound() <-chan Message { return b.inbound }

// Listen polls until ctx ends. It drops anything from another chat, another
// topic or a sender nobody allowed, and says so in the log: a message silently
// ignored looks the same as a message lost.
func (b *Bot) Listen(ctx context.Context) error {
	updates, err := b.bot.UpdatesViaLongPolling(ctx, &telego.GetUpdatesParams{
		Timeout:        pollSeconds,
		AllowedUpdates: []string{"message"},
	})
	if err != nil {
		return fmt.Errorf("start telegram polling: %w", err)
	}

	b.log.Info("listening", zap.Int64("chat_id", b.chatID), zap.Int("topic_id", b.topicID))

	for {
		select {
		case <-ctx.Done():
			close(b.inbound)
			return nil
		case update, ok := <-updates:
			if !ok {
				close(b.inbound)
				return nil
			}
			msg := update.Message
			if msg == nil || msg.Text == "" {
				continue
			}
			if msg.Chat.ID != b.chatID {
				b.log.Info("message from another chat, ignored",
					zap.Int64("chat_id", msg.Chat.ID))
				continue
			}
			if b.topicID != 0 && msg.MessageThreadID != b.topicID {
				b.log.Info("message from another topic, ignored",
					zap.Int("topic_id", msg.MessageThreadID),
					zap.Int("listening_to", b.topicID))
				continue
			}
			if msg.From == nil || !b.allowed[msg.From.ID] {
				b.log.Warn("message from someone not allowed to talk to the session",
					zap.Int64("chat_id", msg.Chat.ID))
				continue
			}

			select {
			case b.inbound <- Message{Text: msg.Text, UserID: msg.From.ID, Username: msg.From.Username}:
			default:
				b.log.Error("inbound buffer is full, message dropped", zap.String("from", msg.From.Username))
			}
		}
	}
}

// Edit replaces the text of a message this bot sent. The harness uses it for the
// one line that says what the session is doing right now, so the chat carries a
// changing status instead of a page of tool names.
func (b *Bot) Edit(ctx context.Context, messageID int, text string) error {
	if text == "" {
		return fmt.Errorf("refusing to edit a message to nothing")
	}

	_, err := b.bot.EditMessageText(ctx, &telego.EditMessageTextParams{
		ChatID:    telego.ChatID{ID: b.chatID},
		MessageID: messageID,
		Text:      text,
	})
	if err != nil {
		return fmt.Errorf("edit telegram message %d: %w", messageID, err)
	}

	return nil
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

const (
	pollSeconds   = 30
	inboundBuffer = 32
)
