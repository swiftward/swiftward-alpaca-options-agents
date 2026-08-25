package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/account"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/record"
)

func TestHealthAndState(t *testing.T) {
	state := record.NewMemory()
	require.NoError(t, state.AppendIntent(context.Background(), record.Intent{
		At:        time.Date(2026, 9, 2, 19, 30, 0, 0, time.UTC),
		Session:   "event",
		Thesis:    "implied volatility collapses after the report",
		Structure: "four legs on AVGO",
		MaxLoss:   "1% of capital",
	}))

	handler, err := Read{Record: state, Log: zaptest.NewLogger(t)}.Handler()
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/state", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var got record.State
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Intents, 1)
	assert.Equal(t, "event", got.Intents[0].Session)
	assert.Equal(t, "1% of capital", got.Intents[0].MaxLoss)
}

func TestServesBuiltPage(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>the record</h1>"), 0o600))

	handler, err := Read{Record: record.NewMemory(), WebDir: dir, Log: zaptest.NewLogger(t)}.Handler()
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "the record")
}

// A configured directory that does not exist is a broken deployment, and saying
// so at start beats serving 404s that look like an empty page.
func TestMissingWebDirRefuses(t *testing.T) {
	_, err := Read{
		Record: record.NewMemory(),
		WebDir: filepath.Join(t.TempDir(), "absent"),
		Log:    zaptest.NewLogger(t),
	}.Handler()
	require.Error(t, err)
}

type brokerDouble struct {
	account   marketdata.Account
	positions []marketdata.Position
	orders    []marketdata.Order
	failure   error
	askedFor  int
}

func (b *brokerDouble) Account(context.Context) (marketdata.Account, error) {
	return b.account, b.failure
}

func (b *brokerDouble) Positions(context.Context) ([]marketdata.Position, error) {
	return b.positions, b.failure
}

func (b *brokerDouble) Orders(_ context.Context, limit int) ([]marketdata.Order, error) {
	b.askedFor = limit
	return b.orders, b.failure
}

func TestMoneyCarriesTheAccountThePositionsAndTheOrders(t *testing.T) {
	broker := &brokerDouble{
		account:   marketdata.Account{Number: "PA3KVT8TYI6V", Equity: 99999.44, EquityYesterday: 100000},
		positions: []marketdata.Position{{Symbol: "SPY260825P00760000", Quantity: -1, UnrealizedPL: 12.5}},
		orders:    []marketdata.Order{{ID: "4530b033", Status: "canceled", Class: "mleg"}},
	}
	handler, err := Read{
		Record: record.NewMemory(), Broker: broker, OrdersShown: 7, Log: zaptest.NewLogger(t),
	}.Handler()
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/money", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var got struct {
		Account   marketdata.Account    `json:"account"`
		Positions []marketdata.Position `json:"positions"`
		Orders    []marketdata.Order    `json:"orders"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "PA3KVT8TYI6V", got.Account.Number)
	require.Len(t, got.Positions, 1)
	require.Len(t, got.Orders, 1)
	assert.Equal(t, 7, broker.askedFor, "the page carries the number of orders it was told to")
}

// A page showing an account with no positions beside it would read as an agent
// holding nothing. One failure fails the whole answer.
func TestMoneyRefusesWhenTheBrokerCannotAnswer(t *testing.T) {
	handler, err := Read{
		Record: record.NewMemory(),
		Broker: &brokerDouble{failure: errors.New("the broker is down")},
		Log:    zaptest.NewLogger(t),
	}.Handler()
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/money", nil))

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

// A deployment with no broker says so rather than answering with zeros, which
// would read as an empty account.
func TestMoneyWithoutABrokerSaysSo(t *testing.T) {
	handler, err := Read{Record: record.NewMemory(), Log: zaptest.NewLogger(t)}.Handler()
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/money", nil))

	assert.Equal(t, http.StatusNotImplemented, rec.Code)
}

type historyDouble struct {
	snapshots []account.Snapshot
	since     time.Time
}

func (h *historyDouble) Since(_ context.Context, since time.Time) ([]account.Snapshot, error) {
	h.since = since
	return h.snapshots, nil
}

func TestTheEquityLineIsDrawnFromTheRecordedHistory(t *testing.T) {
	line := &historyDouble{snapshots: []account.Snapshot{
		{RecordedAt: time.Date(2026, 8, 28, 14, 0, 0, 0, time.UTC), Equity: 100000},
		{RecordedAt: time.Date(2026, 8, 28, 15, 0, 0, 0, time.UTC), Equity: 100420},
	}}
	handler, err := Read{
		Record: record.NewMemory(), History: line, HistoryDays: 3, Log: zaptest.NewLogger(t),
	}.Handler()
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/equity", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var got []account.Snapshot
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got, 2)
	assert.InDelta(t, 100420, got[1].Equity, 1e-9)
	assert.WithinDuration(t, time.Now().AddDate(0, 0, -3), line.since, time.Minute)
}
