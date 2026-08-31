package takeprofit

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/structures"
)

// Every shape a declaration can open, put to the guard that decides what this
// watch may close.
//
// The point is not the three cases below; it is that adding a fourth shape to
// structures.All fails this test until somebody says what the watch does with it.
// That decision was the one nobody made when the convexity layer arrived: the
// guard went on counting legs, and a backspread counts as a vertical.
func TestEveryShapeHasAVerdictFromTheProfitWatch(t *testing.T) {
	for _, shape := range structures.All {
		t.Run(shape.Name, func(t *testing.T) {
			held := []marketdata.Position{
				{
					Symbol: "SPY260903P00744000", AssetClass: "us_option",
					Quantity: -float64(shape.Sold), AverageEntryPrice: 0.15,
				},
				{
					Symbol: "SPY260903P00726000", AssetClass: "us_option",
					Quantity: float64(shape.Bought), AverageEntryPrice: 0.06,
				},
			}

			grouped, declined := Group(held)
			if shape.ClosedByTheProfitWatch {
				assert.Len(t, grouped, 1, "the watch owns closing this shape")
				assert.Empty(t, declined)

				return
			}
			assert.Empty(t, grouped, "this shape belongs to whoever opened it")
			assert.Equal(t, []string{"SPY-2026-09-03-put"}, declined,
				"and it is named as declined rather than passed over in silence")
		})
	}
}

// The list is not allowed to be empty or to lose the ordinary case, or the test
// above would pass by having nothing to say.
func TestTheShapeListCoversWhatWeActuallyTrade(t *testing.T) {
	seen := map[string]bool{}
	for _, shape := range structures.All {
		seen[fmt.Sprintf("%d:%d", shape.Sold, shape.Bought)] = true
	}
	assert.True(t, seen["1:1"], "the credit vertical is most of what we open")
	assert.True(t, seen["1:2"], "the convexity layer opens these")
}
