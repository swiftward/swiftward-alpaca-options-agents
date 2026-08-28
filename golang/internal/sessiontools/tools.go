// Package sessiontools is the MCP server this project runs for its own agent. It
// carries what the broker's server cannot: the intent a session states before it
// orders anything, the record that same session reads when it wakes again, the
// wake-ups it sets for itself, and the volatility history nobody sells.
//
// It never reaches the broker. Orders go through the policy gateway and the
// broker's own MCP server, which is what the hackathon requires.
package sessiontools

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/declaration"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/record"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/screener"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/volatility"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/wakeup"
)

const (
	name    = "swiftward-alpaca-options-agents"
	version = "v0.1.0"
)

type recordIntentInput struct {
	Thesis    string `json:"thesis" jsonschema:"why this trade, in one sentence"`
	Structure string `json:"structure" jsonschema:"the option structure about to be opened"`
	MaxLoss   string `json:"max_loss" jsonschema:"the largest loss this structure can produce"`
	// Required, and it is the price the SESSION read, not one the record can
	// reconstruct later: the defence windows measure how far the underlying has
	// travelled since the intent, and only the starting point is unrecoverable.
	UnderlyingPrice string `json:"underlying_price" jsonschema:"what the underlying costs right now, as the session just read it"`
}

type recordIntentOutput struct {
	RecordedAt time.Time `json:"recorded_at" jsonschema:"when the intent was stored"`
}

type readStateInput struct{}

type scheduleOutput struct {
	Sessions []declaration.Scheduled `json:"sessions" jsonschema:"every session that wakes you, in the order declared"`
}

type volatilityInput struct {
	Underlying string `json:"underlying" jsonschema:"the symbol whose option volatility to look at, for example SPY"`
	Days       int    `json:"days,omitempty" jsonschema:"how many days back to look; the whole recorded history if left out"`
}

// defaultVolatilityDays is how far back the history is read when the session did
// not say. The record starts on kickoff day, so this is the whole of it.
const defaultVolatilityDays = 30

type postToChatInput struct {
	Text string `json:"text" jsonschema:"what the people watching this session should read"`
}

type postToChatOutput struct {
	MessageID int `json:"message_id" jsonschema:"the id Telegram gave the message"`
}

// Poster is the chat this session can reach. A nil one means no chat was
// configured, and the tool is then not offered at all: an agent that can see a
// tool assumes it works.
type Poster interface {
	Send(ctx context.Context, text string) (int, error)
}

// Wakeups is the session's standing requests to be woken. A nil one means the
// harness is not holding a clock for this session, and the tools are not offered.
type Wakeups interface {
	AddAt(cause wakeup.Cause, at time.Time, now time.Time) (wakeup.Wakeup, error)
	AddPrice(cause wakeup.Cause, symbol string, direction wakeup.Direction, level float64, now time.Time) (wakeup.Wakeup, error)
	List() []wakeup.Wakeup
	Cancel(id string) error
}

type wakeAtInput struct {
	Cause string `json:"cause" jsonschema:"why you want to be woken then, in one sentence"`
	At    string `json:"at" jsonschema:"when, as a time like 2026-09-04T09:35:00-04:00"`
}

type wakeOnPriceInput struct {
	Cause     string  `json:"cause" jsonschema:"why you want to be woken then, in one sentence"`
	Symbol    string  `json:"symbol" jsonschema:"the symbol to watch, for example SPY"`
	Direction string  `json:"direction" jsonschema:"above or below"`
	Level     float64 `json:"level" jsonschema:"the price that wakes you"`
}

type wakeupOutput struct {
	ID string `json:"id" jsonschema:"the identifier to cancel it with"`
}

type cancelWakeupInput struct {
	ID string `json:"id" jsonschema:"the identifier from the list"`
}

type noInput struct{}

// Volatility is the history of what the market charged for options. A nil one
// means nothing has been recorded on this deployment and the tool is not
// offered: an agent that can see a tool assumes it answers.
type Volatility interface {
	Summarise(ctx context.Context, underlying string, since time.Time) (volatility.Summary, error)
}

// Schedule is what the declaration says wakes this agent. Nil means no schedule
// is held here and the tool is not offered.
type Schedule interface {
	Schedule() []declaration.Scheduled
}

// Running says which turn is in flight and who woke it. Nil means nothing here
// knows - a session running without a harness - and the intent is then recorded
// without one rather than with a name the model invented.
type Running interface {
	RunningTurn() (ref string, wokenBy string)
}

// Tools is what this session is given. A field left nil is a tool the session is
// never shown, which is the only honest way to offer something this deployment
// cannot do.
type Tools struct {
	// Record is where an intent is written and the past is read.
	Record record.Keeper
	// Now is the clock the tools stamp with. It is a field so a test is not at the
	// mercy of the wall clock.
	Now func() time.Time
	// Chat is offered only when no harness is running: with one, everything the
	// session says is already posted, and a tool as well would double it.
	Chat Poster
	// Wakeups are the session's own standing requests.
	Wakeups Wakeups
	// Volatility answers where today's implied volatility sits in its own history.
	Volatility Volatility
	// Schedule answers when this agent will be woken and why.
	Schedule Schedule
	// Running says which turn the session is inside.
	Running Running
	// Shortlist is what the screener priced across the whole universe. A nil one
	// means no screener is running and the tool is not offered.
	Shortlist Shortlist
	// SweepEvery is how often the screener starts a pass. It is here so the list
	// can say whether it is FRESH, instead of handing a session a number of
	// seconds and leaving it to guess.
	//
	// Guessing went wrong on 27 August in the way that costs a trade: the entry
	// window read the list twice, saw the age go from 280 seconds to 309, and
	// concluded the screener had stopped - so by the rule below it treated the
	// list as absent and sent nothing. The age of course grows between two reads
	// inside one pass; that is not a fault, it is the interval. A session cannot
	// tell "stopped" from "read twice in one cycle" out of one number, and it
	// should not have to.
	SweepEvery time.Duration
	// Asked answers whether a tool was called during one turn. Nil skips the
	// check, which is what a run without a database does.
	Asked Asked
}

// Asked answers whether a tool was called during one turn.
type Asked interface {
	AskedInTurn(ctx context.Context, turnRef, tool string) (bool, error)
}

// envelopeTool is the name a session calls to learn its limits. Named here
// because the check below is about that call and no other.
const envelopeTool = "read_envelope"

// Shortlist is the last sweep of the universe, richest first.
type Shortlist interface {
	Candidates(ctx context.Context, most int) ([]screener.Candidate, time.Time, error)
}

// running names the turn an intent belongs to.
func (t Tools) running() (ref string, wokenBy string) {
	if t.Running == nil {
		return "", ""
	}

	return t.Running.RunningTurn()
}

// Handler serves these tools over Streamable HTTP.
func (t Tools) Handler() http.Handler {
	state, now, poster, wakeups := t.Record, t.Now, t.Chat, t.Wakeups
	server := mcp.NewServer(&mcp.Implementation{Name: name, Version: version}, nil)

	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "record_intent",
			Description: "State what this session is about to do and the loss it accepts, before sending any order.",
		},
		func(ctx context.Context, req *mcp.CallToolRequest, in recordIntentInput) (*mcp.CallToolResult, recordIntentOutput, error) {
			if in.Thesis == "" || in.Structure == "" || in.MaxLoss == "" {
				return nil, recordIntentOutput{}, fmt.Errorf("thesis, structure and max_loss are all required")
			}
			// Refused rather than defaulted. A window that wakes when price has
			// come a third of the way to the strike cannot compute that from an
			// empty cell, and an intent recorded without it silently disables the
			// defence it was supposed to arm.
			if in.UnderlyingPrice == "" {
				return nil, recordIntentOutput{}, fmt.Errorf(
					"underlying_price is required: state what the underlying costs now, as you just read it, or the windows that watch this position have nothing to measure against")
			}
			at := now()
			turn, session := t.running()

			// The limits have to have been read in THIS turn. Sessions are turns on
			// one conversation, so an answer from an earlier turn is still in the
			// model's context and does not read as stale to it - which is why this
			// is checked rather than asked for. A session that states an intent
			// from memory is stating it against limits that may have moved.
			if t.Asked != nil && turn != "" {
				asked, err := t.Asked.AskedInTurn(ctx, turn, envelopeTool)
				if err != nil {
					return nil, recordIntentOutput{}, err
				}
				if !asked {
					return nil, recordIntentOutput{}, fmt.Errorf(
						"call %s in this turn before recording an intent: limits change while a conversation runs, and an answer from an earlier turn is not this turn's answer",
						envelopeTool)
				}
			}

			// The same structure stated twice in one turn is one decision written
			// down twice, and a judge reads it as two. The session is told, so it
			// orders rather than restating.
			if turn != "" {
				current, err := state.Read(ctx)
				if err != nil {
					return nil, recordIntentOutput{}, err
				}
				for _, already := range current.Intents {
					if already.TurnRef == turn && already.Structure == in.Structure {
						return nil, recordIntentOutput{}, fmt.Errorf(
							"this turn already recorded an intent for %q at %s: order it, or state a different structure",
							in.Structure, already.At.Format(time.RFC3339))
					}
				}
			}
			if err := state.AppendIntent(ctx, record.Intent{
				At:        at,
				TurnRef:   turn,
				Session:   session,
				Thesis:    in.Thesis,
				Structure: in.Structure,
				MaxLoss:   in.MaxLoss,

				UnderlyingPrice: in.UnderlyingPrice,
			}); err != nil {
				return nil, recordIntentOutput{}, err
			}
			return nil, recordIntentOutput{RecordedAt: at}, nil
		})

	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "read_state",
			Description: "Read what earlier sessions did: their turns, the intents they recorded before ordering, and the refusals they were given.",
		},
		func(ctx context.Context, req *mcp.CallToolRequest, _ readStateInput) (*mcp.CallToolResult, stateAnswer, error) {
			current, err := state.Read(ctx)
			if err != nil {
				return nil, stateAnswer{}, err
			}
			return nil, answerWith(current), nil
		})

	if t.Schedule != nil {
		mcp.AddTool(server,
			&mcp.Tool{
				Name:        "read_schedule",
				Description: "Read when you will be woken and why, as declared. This is the whole schedule: nothing else wakes you except a person writing to you and the wake-ups you set yourself.",
			},
			func(ctx context.Context, req *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, scheduleOutput, error) {
				return nil, scheduleOutput{Sessions: t.Schedule.Schedule()}, nil
			})
	}

	if t.Shortlist != nil {
		mcp.AddTool(server,
			&mcp.Tool{
				Name: "read_candidates",
				Description: "Read the structures the screener priced across the whole permitted universe on its last sweep, best first. " +
					"Each carries what it pays, what it risks, how far the sold strike sits from the price, what crossing the book costs, and credit_after_cost - the credit with half that crossing taken out, which is what an order sent at the midpoint is worth in expectation. " +
					"edge_points is measured from credit_after_cost, so a structure quoted wide already shows a worse number and needs no separate rule about its cost. " +
					"edge_from names what the chance of surviving was read from: the broker's delta, or the price of volatility on the day a contract expires, when the broker computes no delta. " +
					"seconds_old says how long ago the sweep that priced this list was taken, and fresh says whether that is normal for this deployment - the sweeps come at an interval, so a list is routinely a few minutes old and that is not a fault. Trust fresh; do not work it out from seconds_old, and do not read a rising age across two reads as the screener having stopped, because inside one interval the age rises by design. " +
					"Age still matters for the prices: seven minutes was enough to turn +7.5 points of edge into -7.2 on one of these structures, so re-read the legs before ordering whatever fresh says. A list that comes back fresh=false is no list at all. " +
					"This is what the market offers, not what you should take: the choice, the size and whether to trade at all remain yours.",
			},
			func(ctx context.Context, req *mcp.CallToolRequest, in candidatesInput) (*mcp.CallToolResult, candidatesAnswer, error) {
				most := in.Most
				if most <= 0 {
					most = defaultCandidates
				}
				found, takenAt, err := t.Shortlist.Candidates(ctx, most)
				if err != nil {
					return nil, candidatesAnswer{}, err
				}
				// Age, not the timestamp: a session reasoning about "how stale is
				// this" should not first have to work out what time it is.
				age := 0
				if !takenAt.IsZero() && len(found) > 0 {
					age = int(t.Now().Sub(takenAt).Seconds())
				}

				return nil, candidatesAnswer{
					Candidates: found,
					SecondsOld: age,
					Fresh:      t.fresh(takenAt, found),
				}, nil
			})
	}

	if t.Volatility != nil {
		mcp.AddTool(server,
			&mcp.Tool{
				Name:        "read_volatility_history",
				Description: "Ask where the implied volatility of an underlying sits inside its own recent history: the latest reading, the lowest, the median, the highest, and its rank from 0 to 100.",
			},
			func(ctx context.Context, req *mcp.CallToolRequest, in volatilityInput) (*mcp.CallToolResult, volatility.Summary, error) {
				if in.Underlying == "" {
					return nil, volatility.Summary{}, fmt.Errorf("underlying is required")
				}
				days := in.Days
				if days <= 0 {
					days = defaultVolatilityDays
				}
				summary, err := t.Volatility.Summarise(ctx, strings.ToUpper(in.Underlying),
					now().AddDate(0, 0, -days))
				if err != nil {
					return nil, volatility.Summary{}, err
				}
				return nil, summary, nil
			})
	}

	if poster != nil {
		mcp.AddTool(server,
			&mcp.Tool{
				Name:        "post_to_chat",
				Description: "Tell the people watching this session something they should read now.",
			},
			func(ctx context.Context, req *mcp.CallToolRequest, in postToChatInput) (*mcp.CallToolResult, postToChatOutput, error) {
				id, err := poster.Send(ctx, in.Text)
				if err != nil {
					return nil, postToChatOutput{}, err
				}
				return nil, postToChatOutput{MessageID: id}, nil
			})
	}

	if wakeups != nil {
		mcp.AddTool(server,
			&mcp.Tool{
				Name:        "wake_me_at",
				Description: "Ask to be woken at a time, with the reason you will need then.",
			},
			func(ctx context.Context, req *mcp.CallToolRequest, in wakeAtInput) (*mcp.CallToolResult, wakeupOutput, error) {
				at, err := time.Parse(time.RFC3339, in.At)
				if err != nil {
					return nil, wakeupOutput{}, fmt.Errorf("at %q is not a time like 2026-09-04T09:35:00-04:00: %w", in.At, err)
				}
				created, err := wakeups.AddAt(wakeup.Cause(in.Cause), at, now())
				if err != nil {
					return nil, wakeupOutput{}, err
				}
				return nil, wakeupOutput{ID: created.ID}, nil
			})

		mcp.AddTool(server,
			&mcp.Tool{
				Name:        "wake_me_on_price",
				Description: "Ask to be woken when a symbol trades through a price, with the reason you will need then.",
			},
			func(ctx context.Context, req *mcp.CallToolRequest, in wakeOnPriceInput) (*mcp.CallToolResult, wakeupOutput, error) {
				created, err := wakeups.AddPrice(wakeup.Cause(in.Cause), in.Symbol, wakeup.Direction(in.Direction), in.Level, now())
				if err != nil {
					return nil, wakeupOutput{}, err
				}
				return nil, wakeupOutput{ID: created.ID}, nil
			})

		mcp.AddTool(server,
			&mcp.Tool{
				Name:        "list_wakeups",
				Description: "List the wake-ups you have standing, with their identifiers.",
			},
			func(ctx context.Context, req *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, []wakeup.Wakeup, error) {
				return nil, wakeups.List(), nil
			})

		mcp.AddTool(server,
			&mcp.Tool{
				Name:        "cancel_wakeup",
				Description: "Cancel a wake-up you no longer need.",
			},
			func(ctx context.Context, req *mcp.CallToolRequest, in cancelWakeupInput) (*mcp.CallToolResult, wakeupOutput, error) {
				if err := wakeups.Cancel(in.ID); err != nil {
					return nil, wakeupOutput{}, err
				}
				return nil, wakeupOutput{ID: in.ID}, nil
			})
	}

	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
}

// defaultCandidates is how many the session is shown when it does not say. Few
// enough to read in a turn, many enough that one unsuitable name does not empty
// the list.
const defaultCandidates = 20

type candidatesInput struct {
	Most int `json:"most,omitempty" jsonschema:"how many to return, richest first; 20 when not given"`
}

type candidatesAnswer struct {
	Candidates []screener.Candidate `json:"candidates" jsonschema:"the structures the last sweep priced, richest first"`
	// SecondsOld is how long ago the sweep behind this list was taken. Rows
	// outlive the sweep that wrote them, so a list an hour old reads exactly like
	// one a minute old unless it says which it is.
	SecondsOld int `json:"seconds_old" jsonschema:"how many seconds ago the sweep behind this list was taken"`
	// Fresh is whether that age is normal for this deployment. It exists so the
	// session is handed an answer rather than a number to reason from: sweeps
	// come at an interval, a list is routinely older than one of them, and one
	// age tells nobody whether the screener is running.
	Fresh bool `json:"fresh" jsonschema:"whether the screener is keeping this list up to date; false means treat it as no list at all"`
}

// fresh answers whether the list is being kept up to date.
//
// The bound is TWO intervals plus a minute, and each part is there for a reason.
// A pass takes time of its own, so at the moment a new one finishes the previous
// list is already one interval plus that duration old; one interval would call
// every healthy list stale. Two plus a minute leaves room for a slow pass and
// still catches a screener that has actually stopped, which shows up as an age
// that keeps climbing past any bound.
func (t Tools) fresh(takenAt time.Time, found []screener.Candidate) bool {
	if len(found) == 0 || takenAt.IsZero() {
		return false
	}
	if t.SweepEvery <= 0 {
		// Nobody said how often the sweep runs, so nothing here can judge it. Say
		// fresh and let the age speak for itself, rather than calling a working
		// list dead on a setting this process was not given.
		return true
	}

	return t.Now().Sub(takenAt) <= 2*t.SweepEvery+time.Minute
}
