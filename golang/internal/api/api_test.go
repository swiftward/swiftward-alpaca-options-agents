package api

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
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
		Thesis:    "implied volatility collapses after the report",
		Structure: "four legs on AVGO",
		MaxLoss:   "1% of capital",
	}))

	handler, err := Read{Record: state, Key: testKey, Log: zaptest.NewLogger(t)}.Handler()
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithKey(http.MethodGet, "/healthz", nil))
	assert.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithKey(http.MethodGet, "/api/state", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var got record.State
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Intents, 1)
	assert.Equal(t, "1% of capital", got.Intents[0].MaxLoss)
}

func TestServesBuiltPage(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>the record</h1>"), 0o600))

	handler, err := Read{Record: record.NewMemory(), WebDir: dir, Key: testKey, Log: zaptest.NewLogger(t)}.Handler()
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithKey(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "the record")
}

// A configured directory that does not exist is a broken deployment, and saying
// so at start beats serving 404s that look like an empty page.
func TestMissingWebDirRefuses(t *testing.T) {
	_, err := Read{
		Record: record.NewMemory(),
		WebDir: filepath.Join(t.TempDir(), "absent"),
		Key:    testKey,
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
		Record: record.NewMemory(), Broker: broker, OrdersShown: 7, Key: testKey, Log: zaptest.NewLogger(t),
	}.Handler()
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithKey(http.MethodGet, "/api/money", nil))
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
		Key:    testKey,
		Log:    zaptest.NewLogger(t),
	}.Handler()
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithKey(http.MethodGet, "/api/money", nil))

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

// A deployment with no broker says so rather than answering with zeros, which
// would read as an empty account.
func TestMoneyWithoutABrokerSaysSo(t *testing.T) {
	handler, err := Read{Record: record.NewMemory(), Key: testKey, Log: zaptest.NewLogger(t)}.Handler()
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithKey(http.MethodGet, "/api/money", nil))

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
		Record: record.NewMemory(), History: line, HistoryDays: 3, Key: testKey, Log: zaptest.NewLogger(t),
	}.Handler()
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithKey(http.MethodGet, "/api/equity", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var got []account.Snapshot
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got, 2)
	assert.InDelta(t, 100420, got[1].Equity, 1e-9)
	assert.WithinDuration(t, time.Now().AddDate(0, 0, -3), line.since, time.Minute)
}

// testKey is the page's key in this file. The read side refuses to start without
// one, so every case here has to carry it - which is the contract, not scaffolding.
const testKey = "test-page-key"

// requestWithKey is httptest.NewRequest with the key a reader must carry.
func requestWithKey(method, target string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, target, body)
	req.Header.Set(keyHeader, testKey)

	return req
}

// The stand served 650 KB of JavaScript and 370 KB of equity history byte for
// byte until 4 September. These four hold the fix in place.
func TestCompressesWhatIsAsked(t *testing.T) {
	handler, err := Read{Record: record.NewMemory(), Key: testKey, Log: zaptest.NewLogger(t)}.Handler()
	require.NoError(t, err)

	req := requestWithKey(http.MethodGet, "/api/state", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
	assert.Equal(t, "Accept-Encoding", rec.Header().Get("Vary"))
	assert.Empty(t, rec.Header().Get("Content-Length"))

	unzipped, err := gzip.NewReader(rec.Body)
	require.NoError(t, err)
	body, err := io.ReadAll(unzipped)
	require.NoError(t, err)

	var got record.State
	require.NoError(t, json.Unmarshal(body, &got))
}

func TestLeavesTheAnswerAloneWhenGzipIsNotAsked(t *testing.T) {
	handler, err := Read{Record: record.NewMemory(), Key: testKey, Log: zaptest.NewLogger(t)}.Handler()
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithKey(http.MethodGet, "/api/state", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Header().Get("Content-Encoding"))

	var got record.State
	assert.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
}

// A range is a request for bytes at an offset of the FILE. Compressed, the
// offsets belong to something else and the reader gets the wrong slice.
func TestLeavesARangeUncompressed(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>the record</h1>"), 0o600))
	// Not index.html: a file server answers that name with a redirect to the
	// directory, and the test would be measuring the redirect.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "robots.txt"), []byte("<h1>the record</h1>"), 0o600))

	handler, err := Read{Record: record.NewMemory(), WebDir: dir, Key: testKey, Log: zaptest.NewLogger(t)}.Handler()
	require.NoError(t, err)

	req := requestWithKey(http.MethodGet, "/robots.txt", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Range", "bytes=0-3")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusPartialContent, rec.Code)
	assert.Empty(t, rec.Header().Get("Content-Encoding"))
	assert.Equal(t, "<h1>", rec.Body.String())
}

// A hashed name never changes its bytes; the file that names it changes daily.
func TestKeepsHashedFilesAndRevalidatesThePage(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "assets"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "assets", "index-abc123.js"), []byte("export {}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>the record</h1>"), 0o600))

	handler, err := Read{Record: record.NewMemory(), WebDir: dir, Key: testKey, Log: zaptest.NewLogger(t)}.Handler()
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithKey(http.MethodGet, "/assets/index-abc123.js", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "public, max-age=31536000, immutable", rec.Header().Get("Cache-Control"))

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithKey(http.MethodGet, "/live", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))
}

// The page ships one HTML file per route, rendered at build time. A reader who
// does not run JavaScript - a judge's agent, most of all - must get the page they
// asked for rather than the landing page's shell under every address.
func TestAPrerenderedRouteServesItsOwnFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("the landing page"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "live"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "live", "index.html"), []byte("the live page"), 0o600))

	handler, err := Read{Record: record.NewMemory(), WebDir: dir, Key: testKey, Log: zaptest.NewLogger(t)}.Handler()
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithKey(http.MethodGet, "/live", nil))
	require.Equal(t, http.StatusOK, rec.Code, "no redirect: a directory must be answered with the page inside it")
	assert.Contains(t, rec.Body.String(), "the live page")

	// A route with no file of its own still falls back to the landing page, which
	// is what makes a mistyped link show where you landed.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithKey(http.MethodGet, "/whatever", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "the landing page")
}
