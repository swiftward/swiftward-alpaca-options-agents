package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// The bot under test is the real telego client; only Telegram's own server is
// replaced, so the request shape this asserts is the one that would go out.
const testToken = "123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func fakeTelegram(t *testing.T, seen *map[string]any) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, seen))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":4242,"date":0,"chat":{"id":-100,"type":"supergroup"}}}`))
	}))
	t.Cleanup(server.Close)

	return server
}

func TestSendCarriesTheTopic(t *testing.T) {
	var seen map[string]any
	server := fakeTelegram(t, &seen)

	bot, err := New(Config{Token: testToken, ChatID: -1003770330300, TopicID: 7287, APIServer: server.URL}, zaptest.NewLogger(t))
	require.NoError(t, err)

	id, err := bot.Send(context.Background(), "entry session opened a put spread")
	require.NoError(t, err)
	assert.Equal(t, 4242, id)
	assert.Equal(t, "entry session opened a put spread", seen["text"])
	assert.EqualValues(t, 7287, seen["message_thread_id"], "without the topic the message lands in General")
}

func TestSendWithoutTopicOmitsIt(t *testing.T) {
	var seen map[string]any
	server := fakeTelegram(t, &seen)

	bot, err := New(Config{Token: testToken, ChatID: 3813730, APIServer: server.URL}, zaptest.NewLogger(t))
	require.NoError(t, err)

	_, err = bot.Send(context.Background(), "hello")
	require.NoError(t, err)
	_, present := seen["message_thread_id"]
	assert.False(t, present)
}

func TestSendRefusesEmptyText(t *testing.T) {
	var seen map[string]any
	server := fakeTelegram(t, &seen)

	bot, err := New(Config{Token: testToken, ChatID: 1, APIServer: server.URL}, zaptest.NewLogger(t))
	require.NoError(t, err)

	_, err = bot.Send(context.Background(), "")
	require.Error(t, err)
	assert.Nil(t, seen, "nothing should have reached Telegram")
}

func TestNewRefusesIncompleteConfig(t *testing.T) {
	cases := map[string]Config{
		"no_token":   {ChatID: 1},
		"no_chat_id": {Token: testToken},
		"neither":    {},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := New(cfg, zaptest.NewLogger(t))
			require.Error(t, err)
			assert.False(t, cfg.Configured())
		})
	}
}

// listenAgainst drives the real polling loop against a fake Telegram that serves
// one batch of updates and then nothing.
func listenAgainst(t *testing.T, cfg Config, updates string) *Bot {
	t.Helper()

	served := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/getUpdates") && !served {
			served = true
			_, _ = w.Write([]byte(updates))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
	}))
	t.Cleanup(server.Close)

	cfg.Token = testToken
	cfg.APIServer = server.URL

	bot, err := New(cfg, zaptest.NewLogger(t))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = bot.Listen(ctx) }()

	return bot
}

func update(chatID int64, topicID int, userID int64, text string) string {
	return fmt.Sprintf(`{"ok":true,"result":[{"update_id":1,"message":{"message_id":9,"date":0,`+
		`"chat":{"id":%d,"type":"supergroup"},"message_thread_id":%d,`+
		`"from":{"id":%d,"is_bot":false,"username":"trader"},"text":%q}}]}`,
		chatID, topicID, userID, text)
}

func TestListenDeliversAnAllowedMessage(t *testing.T) {
	bot := listenAgainst(t,
		Config{ChatID: -100, TopicID: 7, AllowUserIDs: []int64{42}},
		update(-100, 7, 42, "close everything"))

	select {
	case msg := <-bot.Inbound():
		assert.Equal(t, "close everything", msg.Text)
		assert.EqualValues(t, 42, msg.UserID)
	case <-time.After(5 * time.Second):
		t.Fatal("the message never arrived")
	}
}

func TestListenDropsWhatItShould(t *testing.T) {
	cases := map[string]struct {
		cfg    Config
		update string
	}{
		"sender_not_allowed": {
			cfg:    Config{ChatID: -100, AllowUserIDs: []int64{42}},
			update: update(-100, 0, 999, "let me in"),
		},
		"another_chat": {
			cfg:    Config{ChatID: -100, AllowUserIDs: []int64{42}},
			update: update(-200, 0, 42, "wrong room"),
		},
		"another_topic": {
			cfg:    Config{ChatID: -100, TopicID: 7, AllowUserIDs: []int64{42}},
			update: update(-100, 8, 42, "wrong thread"),
		},
		"nobody_allowed": {
			cfg:    Config{ChatID: -100},
			update: update(-100, 0, 42, "an open channel is not a default"),
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			bot := listenAgainst(t, tc.cfg, tc.update)
			select {
			case msg := <-bot.Inbound():
				t.Fatalf("this should not have been delivered: %q", msg.Text)
			case <-time.After(300 * time.Millisecond):
			}
		})
	}
}

func TestEditReplacesTheStatusLine(t *testing.T) {
	var seen map[string]any
	server := fakeTelegram(t, &seen)

	bot, err := New(Config{Token: testToken, ChatID: -100, APIServer: server.URL}, zaptest.NewLogger(t))
	require.NoError(t, err)

	require.NoError(t, bot.Edit(context.Background(), 4242, "working: reading the option chain"))
	assert.EqualValues(t, 4242, seen["message_id"])
	assert.Equal(t, "working: reading the option chain", seen["text"])
}
