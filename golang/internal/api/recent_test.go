package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/record"
)

// counted is a broker that says how many times it was asked, and can be held
// mid-answer so a test can put several readers in the same moment.
type counted struct {
	mu      sync.Mutex
	asked   int
	holdsAt chan struct{}
	failure error
}

func (c *counted) Account(context.Context) (marketdata.Account, error) {
	c.mu.Lock()
	c.asked++
	held := c.holdsAt
	failure := c.failure
	c.mu.Unlock()

	if held != nil {
		<-held
	}

	return marketdata.Account{Equity: 100_000}, failure
}

func (c *counted) Positions(context.Context) ([]marketdata.Position, error) {
	return nil, c.failure
}

func (c *counted) Orders(context.Context, int) ([]marketdata.Order, error) {
	return nil, c.failure
}

func (c *counted) times() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.asked
}

func served(t *testing.T, broker Broker) http.Handler {
	t.Helper()

	handler, err := Read{
		Record: record.NewMemory(), Broker: broker, Key: testKey, Log: zaptest.NewLogger(t),
	}.Handler()
	require.NoError(t, err)

	return handler
}

func asksMoney(t *testing.T, handler http.Handler) int {
	t.Helper()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithKey(http.MethodGet, "/api/money", nil))

	return rec.Code
}

func TestKeepsTheBrokersAnswerForAMoment(t *testing.T) {
	broker := &counted{}
	handler := served(t, broker)

	assert.Equal(t, http.StatusOK, asksMoney(t, handler))
	assert.Equal(t, http.StatusOK, asksMoney(t, handler))
	assert.Equal(t, 1, broker.times(), "the second reader arrived inside the window and should have been answered from the kept copy")
}

func TestAsksAgainWhenTheCopyIsOld(t *testing.T) {
	was := keepsFor
	keepsFor = time.Millisecond
	t.Cleanup(func() { keepsFor = was })

	broker := &counted{}
	handler := served(t, broker)

	assert.Equal(t, http.StatusOK, asksMoney(t, handler))
	time.Sleep(5 * time.Millisecond)
	assert.Equal(t, http.StatusOK, asksMoney(t, handler))
	assert.Equal(t, 2, broker.times(), "the copy had expired and the reader must get a live answer, not an old one")
}

// Judging day is several people on the same page in the same second. They make
// one question of it.
func TestManyReadersMakeOneQuestion(t *testing.T) {
	broker := &counted{holdsAt: make(chan struct{})}
	handler := served(t, broker)

	var readers sync.WaitGroup
	for range 10 {
		readers.Add(1)

		go func() {
			defer readers.Done()
			assert.Equal(t, http.StatusOK, asksMoney(t, handler))
		}()
	}

	// Every reader is now waiting on the one call, which is waiting on this.
	assert.Eventually(t, func() bool { return broker.times() == 1 }, time.Second, time.Millisecond)
	close(broker.holdsAt)
	readers.Wait()

	assert.Equal(t, 1, broker.times())
}

// A broker that is down is news, and news is not cached.
func TestDoesNotKeepAFailure(t *testing.T) {
	broker := &counted{failure: assert.AnError}
	handler := served(t, broker)

	assert.Equal(t, http.StatusServiceUnavailable, asksMoney(t, handler))
	assert.Equal(t, http.StatusServiceUnavailable, asksMoney(t, handler))
	assert.GreaterOrEqual(t, broker.times(), 2, "a failure must not stand in for an answer")
}
