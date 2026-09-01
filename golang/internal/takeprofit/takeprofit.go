// Package takeprofit closes a winning structure when the market will hand back
// most of what it paid for it, without waking the agent to ask.
//
// The split is the same one the ladder already makes, moved from the entry to
// the exit. Deciding WHAT to sell and at what price is judgement and belongs to
// the model. Watching a number every half minute and acting when it crosses a
// line is arithmetic on a clock: it has to happen in seconds, and a turn of the
// agent costs a minute and a half.
//
// Why it exists at all. Until 28 August 2026 nothing in this system closed a
// winning credit spread. `defend` looks only at the BOUGHT strike and only for
// losses; `flatten` closes what sits within fifty cents of the strike at the end
// of the day, which is to say it arrives after the profit is gone; the harvest
// playbook ends at the entry. A QQQ 725/726 spread that day had given back
// seventy-four percent of its credit with nobody looking at it, holding $13,770
// of tail for the remaining $170.
//
// Measured over 553 trades the rule picked: the price reached the sold strike in
// 45 percent of them, and 59 percent of THOSE ended in loss. The quiet trades
// earn $9,741 and the touched ones give back $7,454. Everything this package is
// for lives in that second number.
//
// It can only make the book smaller. It never opens, never adds, never picks
// what to trade.
package takeprofit

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/record"
)

// Broker is what watching needs and nothing more.
type Broker interface {
	Positions(ctx context.Context) ([]marketdata.Position, error)
	Orders(ctx context.Context, limit int) ([]marketdata.Order, error)
	Quotes(ctx context.Context, symbols []string) (map[string]marketdata.Quote, error)
	CloseStructure(ctx context.Context, legs []marketdata.Leg, sets int, limit float64, name string) (string, error)
}

// Watch closes structures that have given back enough of their credit.
// Keeper is where a sent order is written down. Nil records nothing.
type Keeper interface {
	AppendExecutionStep(ctx context.Context, step record.ExecutionStep) error
}

type Watch struct {
	Broker Broker
	// Record keeps every order this watch sends, at the moment it is sent. An
	// order written down only when the ladder later notices it is missing from
	// the record entirely if it is cancelled before that pass.
	Record Keeper
	// Every is how often the book is looked at. Seconds, not minutes: the whole
	// point is to be there when the number crosses.
	Every time.Duration
	// At answers the share of the received credit at which a structure is closed -
	// 0.25 means "close when it can be bought back for a quarter of what it paid".
	//
	// A function rather than a number, and read on every pass, for the same reason
	// the ladder's ceiling is: this is a TRADING decision, so it belongs in the
	// declaration beside the rest of them, and a declaration is re-read while the
	// process runs. Until 29 August it was an environment variable, which put one
	// trading number in a different place from every other one and meant the two
	// accounts could not differ in it without differing in their deployment.
	//
	// Nil switches the watch off, and says so once at startup rather than running
	// and doing nothing.
	At func() (float64, error)
	// Ordered remembers what has already been sent, so a structure is not closed
	// twice while the first order is still walking.
	Now func() time.Time
	// Where is the exchange's calendar. A structure is closeable only while its
	// expiration has not passed, and "has it passed" is a question about New York,
	// not about the machine.
	Where *time.Location
	Log   *zap.Logger

	sent map[string]time.Time
	// declined remembers which holdings this watch has already said it cannot
	// read, so it says each one ONCE. Without it a book carrying two backspreads
	// writes the same two lines every pass - five thousand a day, on top of
	// everything a reader is actually looking for. A holding that goes and comes
	// back is said again, because that is news.
	declined map[string]bool
}

// standing is how long a sent order is remembered. Past it the structure is
// looked at again: an order the book never took should not lock the position out
// of ever being closed.
const standing = 10 * time.Minute

// Structure is a set of legs on one underlying and one expiration that were
// opened together and are worth pricing together.
type Structure struct {
	Underlying string
	Expiration string
	Kind       string
	Legs       []marketdata.Position
	// Credit is what one set brought in, from what the legs were entered at.
	// Negative means the set cost money, and this package leaves those alone:
	// giving back a share of a debit is not the same question.
	Credit float64
	// Sets is how many of it are held.
	Sets int
}

// share is the take-profit level for this pass, with the check that used to run
// at startup. It runs on every pass because the number can now change under a
// running process.
//
// A SHARE of the credit, so above one it is not a share. Written as a percent -
// 35 for 0.35 - it says "close when the buy-back costs no more than thirty-five
// times the credit", which every structure satisfies from the moment it opens:
// the watch would buy back every winner at any price, on its first pass, and the
// log would call it working.
func (w *Watch) share() (float64, error) {
	at, err := w.At()
	if err != nil {
		return 0, fmt.Errorf("read the take-profit share: %w", err)
	}
	if at <= 0 {
		return 0, fmt.Errorf("the take-profit share is %.2f: nothing would ever be closed", at)
	}
	if at > 1 {
		return 0, fmt.Errorf("the take-profit share is %.2f: it is a share of the credit, so 0.35 rather than 35", at)
	}

	return at, nil
}

func (w *Watch) Run(ctx context.Context) error {
	if w.At == nil {
		w.Log.Info("no take-profit share set: winning structures will be held to expiry")
		return nil
	}
	// Asked once here so a deployment that cannot answer at all fails at startup
	// rather than on the first winner of the week. Every pass asks again, because
	// the declaration behind it is re-read while the process runs.
	share, err := w.share()
	if err != nil {
		return err
	}
	if w.Every <= 0 {
		w.Every = 30 * time.Second
	}
	w.sent = map[string]time.Time{}
	w.declined = map[string]bool{}
	w.Log.Info("watching for structures worth closing",
		zap.Float64("at_share_of_credit", share), zap.Duration("every", w.Every))

	ticker := time.NewTicker(w.Every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			w.step(ctx)
		}
	}
}
