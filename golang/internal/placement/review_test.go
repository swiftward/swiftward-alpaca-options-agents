package placement

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Найдено ревью 28 августа 2026. Окна отбирались по волатильности, кончающейся
// последним днём САМОГО окна - то есть окно судили по его же исходу.
//
// История здесь построена так, что разница видна на глаз: тихо, один прыжок в
// середине, тихо. Прыжок случился в тихую погоду, значит окна, поймавшие его,
// обязаны попасть в выборку сегодняшнего тихого режима. Со старой индексацией
// они выбрасывались - их закрывающая волатильность взлетала, - и хвост, ради
// которого бэкспред покупают, исчезал из выборки, которой его оценивают.
func TestTheRegimeIsJudgedAtTheWindowsOpeningNotItsEnd(t *testing.T) {
	const days = 3

	quiet := func(n int, from float64) []float64 {
		out := make([]float64, n)
		price := from
		for i := range out {
			// Мелкая пила: волатильность заметно больше нуля, но одинаковая.
			if i%2 == 0 {
				price *= 1.001
			} else {
				price *= 0.999
			}
			out[i] = price
		}
		return out
	}

	closes := quiet(300, 700)
	jumpAt := len(closes) / 2
	// Один день, который ходит на десять процентов вверх, и всё после него
	// продолжается от новой цены.
	for i := jumpAt; i < len(closes); i++ {
		closes[i] *= 1.10
	}

	moves, vol, err := windows(closes, days)
	require.NoError(t, err)
	require.Greater(t, vol, 0.0)

	var caught int
	for _, move := range moves {
		if move > 1.05 {
			caught++
		}
	}
	assert.Positive(t, caught,
		"окно, открывшееся в тихую погоду и поймавшее прыжок, принадлежит выборке тихой погоды")
}

// Счёт торговых дней идёт по календарю биржи. Машина команды живёт в UTC+8:
// вечер четверга в Нью-Йорке - это уже утро пятницы здесь, и по местной дате до
// пятничной экспирации не остаётся ни дня.
func TestTheTradingDaysAreCountedOnTheExchangesCalendar(t *testing.T) {
	newYork, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	// Четверг 3 сентября, 20:00 в Нью-Йорке. Локально это уже пятница 08:00.
	evening := time.Date(2026, 9, 4, 8, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))
	friday := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)

	assert.Equal(t, 1, tradingDaysUntil(evening, friday, newYork),
		"в Нью-Йорке ещё четверг, и впереди целая пятничная сессия")
	assert.Equal(t, 0, tradingDaysUntil(evening, friday, time.UTC),
		"а по машинным часам дня уже нет - ровно тот отказ, что ловили")
}

// Котировка, по которой кредит почти равен ширине, - сломанная. Худший случай на
// набор уходит к нулю, наборов помещается сотни, а ожидание линейно по наборам:
// строка, построенная на одном стоячем лоте, возглавляла бы список.
func TestABrokenQuoteDoesNotLeadTheList(t *testing.T) {
	held := aBook("call")
	// Всё как обычно, но одна пара котирована так, что кредит съедает ширину.
	held.priceFor = func(strike float64) (float64, float64) {
		switch {
		case strike == 795:
			return 4.90, 4.95 // непомерный bid у проданной ноги
		case strike == 800:
			return 0.004, 0.005 // и почти даром у купленных
		}
		return 0.02, 0.03
	}

	answer, err := aScorer(held).Score(context.Background(), anAsk())
	require.NoError(t, err)

	for _, at := range answer.Placements {
		assert.False(t, at.ShortStrike == 795 && at.LongStrike == 800,
			"пара со сломанной котировкой не попадает в ответ вовсе")
		assert.LessOrEqual(t, at.Credit, (at.LongStrike-at.ShortStrike)/2,
			"кредит больше половины ширины - это не сделка, это опечатка в книге")
	}
}

// Окна перекрываются: шаг в один день при сроке в несколько. Читателю нужно
// знать, сколько среди них независимых, иначе «лучший процент истории» окажется
// одним эпизодом, посчитанным пять раз.
func TestItSaysHowManyWindowsAreIndependent(t *testing.T) {
	answer, err := aScorer(aBook("call")).Score(context.Background(), anAsk())
	require.NoError(t, err)

	assert.Positive(t, answer.Independent)
	assert.Less(t, answer.Independent, answer.Windows,
		"перекрывающихся окон всегда больше, чем независимых")
	assert.Equal(t, answer.Windows/answer.TradingDays, answer.Independent)
}
