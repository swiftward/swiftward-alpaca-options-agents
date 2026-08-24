package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

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
