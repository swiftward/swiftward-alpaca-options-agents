package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/envelope"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/record"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/screener"
)

const ruleset = `
ruleset_version: "test-1"
agents:
  options-alpha:
    tools:
      place_option_order:
        - rule: max-loss-per-position
          disclosure: boundary
          subject: position_max_loss
          kind: maximum
          value: 15.0
          unit: percent_of_equity
`

// Страница отдаёт пределы ТЕМ ЖЕ вызовом, каким на них отвечают агенту.
//
// Это стоит проверки, а не доверия: пределы, приходящие обнаружением, - главное
// утверждение этого проекта, и если страница покажет их пересказом, однажды
// пересказ разойдётся с тем, по чему на самом деле торгуют. Тогда мы будем
// показывать судье не систему, а рассказ о ней.
func TestThePageShowsTheSameLimitsTheAgentIsGiven(t *testing.T) {
	path := filepath.Join(t.TempDir(), "envelope.yaml")
	require.NoError(t, os.WriteFile(path, []byte(ruleset), 0o600))

	handler := serving(t, Read{
		Record: record.NewMemory(), EnvelopePath: path, EnvelopeIdentity: "options-alpha",
	})

	answer := httptest.NewRecorder()
	handler.ServeHTTP(answer, httptest.NewRequest(http.MethodGet, "/api/limits", nil))
	require.Equal(t, http.StatusOK, answer.Code)

	var shown envelope.Envelope
	require.NoError(t, json.Unmarshal(answer.Body.Bytes(), &shown))

	// То же самое, но взятое прямо у конверта - буква в букву.
	set, err := envelope.Load(path)
	require.NoError(t, err)
	given, err := set.For("options-alpha", "place_option_order")
	require.NoError(t, err)

	assert.Equal(t, given, shown, "страница и агент обязаны видеть одно и то же")
	assert.Equal(t, "test-1", shown.RulesetVersion, "версия правил названа: по ней сверяют отказ")
}

// Без конверта страница говорит об этом прямо, а не отдаёт пустоту, которую
// читатель примет за «пределов нет».
func TestWithoutAnEnvelopeThePageSaysSo(t *testing.T) {
	handler := serving(t, Read{Record: record.NewMemory()})

	answer := httptest.NewRecorder()
	handler.ServeHTTP(answer, httptest.NewRequest(http.MethodGet, "/api/limits", nil))
	// 501, а не 503: это не «временно недоступно», а «в этом развёртывании
	// конверта нет». Разница важна читателю - второе он будет обновлять, первое нет.
	assert.Equal(t, http.StatusNotImplemented, answer.Code)
}

type sweepDouble struct {
	found   []screener.Candidate
	takenAt time.Time
}

func (s sweepDouble) Candidates(context.Context, int) ([]screener.Candidate, time.Time, error) {
	return s.found, s.takenAt, nil
}

// Проход несёт время. Строки переживают проход, который их записал, поэтому
// список часовой давности читается как минутный, пока не сказано, какой он.
func TestTheSweepSaysWhenItWasTaken(t *testing.T) {
	taken := time.Date(2026, 8, 28, 13, 40, 0, 0, time.UTC)
	handler := serving(t, Read{
		Record: record.NewMemory(), OrdersShown: 10,
		Sweep: sweepDouble{found: []screener.Candidate{{Underlying: "SPY"}}, takenAt: taken},
	})

	answer := httptest.NewRecorder()
	handler.ServeHTTP(answer, httptest.NewRequest(http.MethodGet, "/api/sweep", nil))
	require.Equal(t, http.StatusOK, answer.Code)

	var shown struct {
		Candidates []screener.Candidate `json:"candidates"`
		TakenAt    time.Time            `json:"taken_at"`
	}
	require.NoError(t, json.Unmarshal(answer.Body.Bytes(), &shown))
	assert.Len(t, shown.Candidates, 1)
	assert.Equal(t, taken, shown.TakenAt)
}

func serving(t *testing.T, read Read) http.Handler {
	t.Helper()

	read.Log = zaptest.NewLogger(t)
	handler, err := read.Handler()
	require.NoError(t, err)

	return handler
}

// Маршрут страницы отдаётся страницей, а не отказом.
//
// /live - это не файл, а состояние страницы: маршруты живут в браузере.
// Файловый сервер на такой путь отвечает 404, и посетитель, открывший ссылку или
// просто обновивший вкладку, видит ошибку вместо того, что открывал. Проверять
// это надо здесь, потому что иначе оно ломается молча и обнаруживается тем, кто
// пришёл по ссылке.
func TestAPageRouteIsAnsweredByThePage(t *testing.T) {
	web := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(web, "index.html"), []byte("<!doctype html>страница"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(web, "app.js"), []byte("// настоящий файл"), 0o600))

	handler := serving(t, Read{Record: record.NewMemory(), WebDir: web})

	for _, route := range []string{"/", "/live", "/whatever/deep"} {
		answer := httptest.NewRecorder()
		handler.ServeHTTP(answer, httptest.NewRequest(http.MethodGet, route, nil))
		require.Equal(t, http.StatusOK, answer.Code, route)
		assert.Contains(t, answer.Body.String(), "страница", "%s должен отдавать страницу", route)
	}

	// Настоящий файл остаётся собой: подмена его страницей сломала бы сборку.
	answer := httptest.NewRecorder()
	handler.ServeHTTP(answer, httptest.NewRequest(http.MethodGet, "/app.js", nil))
	assert.Contains(t, answer.Body.String(), "настоящий файл")

	// Данные под /api не попадают под это правило: отказ там должен оставаться
	// отказом, а не превращаться в страницу с кодом 200.
	data := httptest.NewRecorder()
	handler.ServeHTTP(data, httptest.NewRequest(http.MethodGet, "/api/limits", nil))
	assert.Equal(t, http.StatusNotImplemented, data.Code)
}

// Пустой проход отдаётся пустым СПИСКОМ, а не null.
//
// Это не педантизм: 28 августа боевая /live показала белый экран целиком именно
// на этом. Проходов на сервере ещё не было, обработчик отдал `candidates: null`,
// страница сделала по нему `.length` и умерла. Читатель, получивший null, вдобавок
// не отличит «прохода ещё не было» от «это поле сломалось».
func TestAnEmptySweepIsAnEmptyListNotNull(t *testing.T) {
	handler := serving(t, Read{
		Record: record.NewMemory(), OrdersShown: 10,
		Sweep: sweepDouble{found: nil, takenAt: time.Time{}},
	})

	answer := httptest.NewRecorder()
	handler.ServeHTTP(answer, httptest.NewRequest(http.MethodGet, "/api/sweep", nil))
	require.Equal(t, http.StatusOK, answer.Code)

	assert.Contains(t, answer.Body.String(), `"candidates":[]`,
		"пустой список, а не null: на null страница падает целиком")
}
