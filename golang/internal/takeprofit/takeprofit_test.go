package takeprofit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
)

type brokerDouble struct {
	held   []marketdata.Position
	orders []marketdata.Order
	quotes map[string]marketdata.Quote
	sent   []closed
	fail   error
}

type closed struct {
	legs  []marketdata.Leg
	sets  int
	limit float64
}

func (b *brokerDouble) Positions(context.Context) ([]marketdata.Position, error) { return b.held, nil }
func (b *brokerDouble) Orders(context.Context, int) ([]marketdata.Order, error)  { return b.orders, nil }
func (b *brokerDouble) Quotes(_ context.Context, _ []string) (map[string]marketdata.Quote, error) {
	return b.quotes, nil
}

func (b *brokerDouble) CloseStructure(_ context.Context, legs []marketdata.Leg, sets int, limit float64, _ string) error {
	if b.fail != nil {
		return b.fail
	}
	b.sent = append(b.sent, closed{legs: legs, sets: sets, limit: limit})
	return nil
}

func leg(symbol string, qty, entry float64) marketdata.Position {
	return marketdata.Position{
		Symbol: symbol, AssetClass: "us_option", Quantity: qty, AverageEntryPrice: entry,
	}
}

func quote(bid, ask float64) marketdata.Quote { return marketdata.Quote{Bid: bid, Ask: ask} }

// Спред 725/726 на 170 наборов, взятый за кредит 0.19 - тот самый, что 28 августа
// отдал 74% кредита и никем не был замечен.
func theQQQSpread() []marketdata.Position {
	return []marketdata.Position{
		leg("QQQ260828C00725000", -170, 0.50),
		leg("QQQ260828C00726000", 170, 0.31),
	}
}

func watching(b *brokerDouble, at float64) *Watch {
	newYork, _ := time.LoadLocation("America/New_York")
	return &Watch{
		Broker: b, At: at, Every: time.Second,
		// Полдень 28 августа в Нью-Йорке: день экспирации спреда QQQ ещё идёт.
		Now:   func() time.Time { return time.Date(2026, 8, 28, 16, 0, 0, 0, time.UTC) },
		Where: newYork,
		Log:   zap.NewNop(), sent: map[string]time.Time{},
	}
}

// Истёкшую конструкцию закрыть нельзя: она ждёт расчёта. Без этой проверки сторож
// 29 августа отправил ПЯТЬ заявок на спред QQQ, истёкший накануне, по одной
// каждые десять минут, и лестница отменяла каждую по терпению.
//
// Условие про кредит от этого не спасало, а подгоняло: у истёкшей конструкции
// выкуп идёт к нулю, то есть она выглядит созревшей идеально.
func TestAnExpiredStructureIsNotClosed(t *testing.T) {
	newYork, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	b := &brokerDouble{held: theQQQSpread(), quotes: map[string]marketdata.Quote{
		"QQQ260828C00725000": quote(0.01, 0.02),
		"QQQ260828C00726000": quote(0.01, 0.02),
	}}
	w := watching(b, 0.5)
	w.Where = newYork
	// Следующий день на бирже.
	w.Now = func() time.Time { return time.Date(2026, 8, 29, 16, 0, 0, 0, time.UTC) }

	w.step(context.Background())
	assert.Empty(t, b.sent, "истёкшее ждёт расчёта, а не заявки")
}

// А в САМ день экспирации закрывать можно до самого звонка.
func TestOnTheDayOfExpiryItStillCloses(t *testing.T) {
	b := &brokerDouble{held: theQQQSpread(), quotes: map[string]marketdata.Quote{
		"QQQ260828C00725000": quote(0.09, 0.10),
		"QQQ260828C00726000": quote(0.02, 0.03),
	}}
	watching(b, 0.5).step(context.Background())
	assert.Len(t, b.sent, 1)
}

func TestItReadsTheStructureOutOfWhatIsHeld(t *testing.T) {
	got := Group(theQQQSpread())
	require.Len(t, got, 1)

	s := got[0]
	assert.Equal(t, "QQQ", s.Underlying)
	assert.Equal(t, "2026-08-28", s.Expiration)
	assert.Equal(t, "call", s.Kind)
	assert.Equal(t, 170, s.Sets, "170 коротких и 170 длинных - это 170 наборов один к одному")
	assert.InDelta(t, 0.19, s.Credit, 1e-9, "продали 0.50, купили 0.31: кредит 0.19 за набор")
}

// Бэкспред: шесть проданных против двенадцати купленных - это ШЕСТЬ наборов
// один к двум, а не двенадцать половинок. Ошибка здесь закрыла бы вдвое больше,
// чем держим.
func TestSetsAreCountedByTheGreatestCommonDivisor(t *testing.T) {
	got := Group([]marketdata.Position{
		leg("SPY260902C00777000", -6, 1.34),
		leg("SPY260902C00780000", 12, 0.63),
	})
	require.Len(t, got, 1)
	assert.Equal(t, 6, got[0].Sets)
	assert.InDelta(t, 1.34-2*0.63, got[0].Credit, 1e-9)
}

// Выкуп считается по сторонам книги, которые заявка пересечёт: проданное
// выкупается по ask, купленное продаётся по bid. По серединам вышло бы дешевле,
// и закрытие срабатывало бы там, где на самом деле не может.
func TestTheBuyBackIsPricedAtTheSidesAnOrderCrosses(t *testing.T) {
	s := Group(theQQQSpread())[0]
	cost, ok := BuyBack(s, map[string]marketdata.Quote{
		"QQQ260828C00725000": quote(0.09, 0.10),
		"QQQ260828C00726000": quote(0.02, 0.03),
	})
	require.True(t, ok)
	assert.InDelta(t, 0.08, cost, 1e-9, "выкупить 725 по 0.10, продать 726 по 0.02")
}

func TestItClosesWhenEnoughOfTheCreditIsBack(t *testing.T) {
	b := &brokerDouble{held: theQQQSpread(), quotes: map[string]marketdata.Quote{
		"QQQ260828C00725000": quote(0.09, 0.10),
		"QQQ260828C00726000": quote(0.02, 0.03),
	}}
	// Выкуп 0.08 против кредита 0.19 - это 42%, порог 0.5 пройден.
	watching(b, 0.5).step(context.Background())

	require.Len(t, b.sent, 1)
	sent := b.sent[0]
	assert.Equal(t, 170, sent.sets)
	assert.InDelta(t, -0.08, sent.limit, 1e-9, "закрытие за дебет, значит цена отрицательна")
	require.Len(t, sent.legs, 2)
	for _, l := range sent.legs {
		assert.Equal(t, 1, l.Ratio)
		assert.Equal(t, l.Symbol == "QQQ260828C00725000", l.Buy,
			"проданную ногу выкупаем, купленную продаём")
	}
}

func TestItLeavesAStructureThatHasNotGivenEnoughBack(t *testing.T) {
	b := &brokerDouble{held: theQQQSpread(), quotes: map[string]marketdata.Quote{
		"QQQ260828C00725000": quote(0.30, 0.32),
		"QQQ260828C00726000": quote(0.14, 0.16),
	}}
	// Выкуп 0.18 против кредита 0.19 - почти ничего не отдано.
	watching(b, 0.5).step(context.Background())
	assert.Empty(t, b.sent)
}

// Структура, взятая за ДЕБЕТ, этим правилом не управляется: доля отданного
// кредита там не считается, и закрывать её по этому порогу значит закрывать слой
// выпуклости в произвольный момент.
func TestAStructureBoughtForADebitIsLeftAlone(t *testing.T) {
	b := &brokerDouble{
		held: []marketdata.Position{
			leg("SPY260902C00777000", -6, 0.30),
			leg("SPY260902C00780000", 12, 0.40),
		},
		quotes: map[string]marketdata.Quote{
			"SPY260902C00777000": quote(0.01, 0.02),
			"SPY260902C00780000": quote(0.01, 0.02),
		},
	}
	watching(b, 0.5).step(context.Background())
	assert.Empty(t, b.sent)
}

// Заявка, уже идущая к книге, - это та же структура в процессе закрытия. Вторая
// закрыла бы вдвое и оставила счёт коротким тем, чего он не держал.
func TestItDoesNotCloseWhatIsAlreadyBeingClosed(t *testing.T) {
	b := &brokerDouble{
		held:   theQQQSpread(),
		orders: []marketdata.Order{{Symbol: "QQQ260828C00725000", Status: "new"}},
		quotes: map[string]marketdata.Quote{
			"QQQ260828C00725000": quote(0.09, 0.10),
			"QQQ260828C00726000": quote(0.02, 0.03),
		},
	}
	watching(b, 0.5).step(context.Background())
	assert.Empty(t, b.sent)
}

func TestItDoesNotSendTheSameCloseTwice(t *testing.T) {
	b := &brokerDouble{held: theQQQSpread(), quotes: map[string]marketdata.Quote{
		"QQQ260828C00725000": quote(0.09, 0.10),
		"QQQ260828C00726000": quote(0.02, 0.03),
	}}
	w := watching(b, 0.5)
	w.step(context.Background())
	w.step(context.Background())
	assert.Len(t, b.sent, 1)
}

// Перевёрнутая книга: дальняя от денег нога котируется ДОРОЖЕ ближней, чего не
// бывает. Выкуп кредитной конструкции не может стоить меньше нуля - она стоит от
// нуля до своей ширины.
//
// Замерено 28 августа на QQQ 725/726: 726-й колл стоял на покупке 0,03 при 725-м
// на продаже 0,02. Сторож считал выкуп в минус цент, слал заявку по невозможной
// цене и получал отмену по терпению - одиннадцать раз за два часа.
func TestAnInvertedBookIsNotPricedToClose(t *testing.T) {
	b := &brokerDouble{held: theQQQSpread(), quotes: map[string]marketdata.Quote{
		"QQQ260828C00725000": quote(0.01, 0.02), // ближняя к деньгам
		"QQQ260828C00726000": quote(0.03, 0.04), // дальняя, и почему-то дороже
	}}
	watching(b, 0.5).step(context.Background())
	assert.Empty(t, b.sent, "по перевёрнутой книге цены закрытия нет")
}

// Нога, у которой книга стоит одной стороной, цены закрытия не имеет. Заявка
// против половины котировки - это подарок, а не сделка.
func TestALegQuotedOnOneSideStopsTheClose(t *testing.T) {
	b := &brokerDouble{held: theQQQSpread(), quotes: map[string]marketdata.Quote{
		"QQQ260828C00725000": quote(0, 0.10),
		"QQQ260828C00726000": quote(0.02, 0.03),
	}}
	watching(b, 0.5).step(context.Background())
	assert.Empty(t, b.sent)
}

// Нулевая доля выключает сторожа целиком и говорит об этом, а не работает вхолостую.
func TestWithoutAShareItDoesNotRun(t *testing.T) {
	b := &brokerDouble{held: theQQQSpread()}
	w := &Watch{Broker: b, At: 0, Log: zap.NewNop(), Now: time.Now}
	require.NoError(t, w.Run(context.Background()))
	assert.Empty(t, b.sent)
}

func TestARefusedCloseIsNotRememberedAsSent(t *testing.T) {
	b := &brokerDouble{held: theQQQSpread(), fail: errors.New("the broker said no"),
		quotes: map[string]marketdata.Quote{
			"QQQ260828C00725000": quote(0.09, 0.10),
			"QQQ260828C00726000": quote(0.02, 0.03),
		}}
	w := watching(b, 0.5)
	w.step(context.Background())
	b.fail = nil
	w.step(context.Background())
	assert.Len(t, b.sent, 1, "отказ не должен запирать структуру навсегда")
}
