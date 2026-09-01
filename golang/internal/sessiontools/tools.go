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
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/declaration"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/placement"
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
	// Optional, and bounded: the name of one of the causes this turn was actually
	// given. A turn is told more than one thing while it runs, and the record can
	// see WHICH cause was in force when a line was written but never which one the
	// session meant to answer - only the session knows that.
	//
	// It is a choice from a closed list rather than free text, which is what keeps
	// it evidence. A name that was never put in front of this turn is refused, so
	// the field cannot become somewhere to type a story.
	Answers string `json:"answers,omitempty" jsonschema:"optional: which of the causes given to this turn this intent answers, by name"`
	// A close is held to fewer rules than an opening, and only the session knows
	// which it is about to send. The two refusals it lifts are named where they
	// are made; both exist to keep an OPENING honest, and neither is worth a
	// position that cannot be left.
	Closing bool `json:"closing,omitempty" jsonschema:"true when this intent is to close or reduce a position rather than open one"`
}

type recordIntentOutput struct {
	RecordedAt time.Time `json:"recorded_at" jsonschema:"when the intent was stored"`
	// TurnRef is the turn this intent was filed under - which is to say, the
	// session's own turn. It is answered here because a session cannot otherwise
	// know it: `read_state` lists turns, and picking its own out of them is a
	// guess. With it, the order that follows can carry the turn in its
	// client_order_id, and a reader can then join what was DECLARED to what was
	// DONE without our record of tool calls - which a session driven through a
	// mailbox does not have at all.
	TurnRef string `json:"turn_ref,omitempty" jsonschema:"the turn this intent belongs to; put it in the order's client_order_id so the order can be matched to it"`
}

type readStateInput struct{}

type scheduleOutput struct {
	Sessions []declaration.Scheduled `json:"sessions" jsonschema:"every session that wakes you, in the order declared"`
}

type volatilityInput struct {
	Underlying string `json:"underlying" jsonschema:"the symbol whose option volatility to look at, for example SPY"`
	Days       int    `json:"days,omitempty" jsonschema:"how many days back to look; the last 30 days if left out"`
}

// defaultVolatilityDays is how far back the history is read when the session did
// not say.
//
// The field's own description said "the whole recorded history if left out",
// which the code has never done - it has always taken thirty days. Today the two
// agree by accident: the record starts on kickoff day and is four days old. They
// would part on the day it turns a month, which is not a day this competition
// has, and a teammate's arena caught it before then.
//
// It matters more than an ordinary comment because a session READS this
// description and plans on it: a model told it is looking at the whole history
// will not think to ask how long that is.
const defaultVolatilityDays = 30

// scorePlacementsInput is deliberately without defaults for the two distances.
// They are LIMITS, they live in the declaration, and a default here would be the
// same number written in a second place - which is how the two drift apart and
// nobody knows which one is in force. A session that has not read its own
// declaration gets a refusal, not a guess.
type scorePlacementsInput struct {
	Underlying string `json:"underlying" jsonschema:"the symbol the structure is built on, for example SPY"`
	Expiration string `json:"expiration" jsonschema:"the day the structure expires, as YYYY-MM-DD; all legs share it"`
	Kind       string `json:"kind" jsonschema:"call or put"`
	Bought     int    `json:"bought,omitempty" jsonschema:"how many are bought against the one sold; two is the backspread and two is the default"`

	ShortLeastSigma  float64 `json:"short_least_sigma" jsonschema:"how far out the sold leg must sit at least, in sigmas of the move expected by expiry; this is YOUR limit, read it from the declaration"`
	ValleyLeastSigma float64 `json:"valley_least_sigma" jsonschema:"how far out the bought strike - the valley, where the worst case sits - must sit at least, in the same sigmas; also yours"`
	ShortMostSigma   float64 `json:"short_most_sigma,omitempty" jsonschema:"how far out to stop looking; four sigmas if left out, past which the quotes are a penny wide and mean nothing"`

	WorstCaseMost float64 `json:"worst_case_most" jsonschema:"the most this position may lose in dollars at the valley; it decides how many sets fit"`
	Most          int     `json:"most,omitempty" jsonschema:"how many placements to return, best first; five if left out"`
}

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

// wakeupsOutput wraps the list because a tool's structured result must be an
// OBJECT. Returning the slice itself declared `"type": ["null","array"]` at the
// top level, and a client that validates the answer against the schema it was
// given refuses it before the session ever sees it:
//
//	structuredContent: expected record, received null   (nothing standing)
//	structuredContent: expected record, received array  (one wake-up standing)
//
// Found 28 August by a session that could set a wake-up and then had no way to
// read back what was standing. The neighbouring read_schedule always wrapped its
// slice and never had the problem.
type wakeupsOutput struct {
	Wakeups []wakeup.Wakeup `json:"wakeups" jsonschema:"the wake-ups standing right now"`
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

// Running says which turn is in flight. Nil means nothing here knows - a session
// running without a harness - and the intent is then recorded without a turn
// rather than with a name the model invented.
//
// Which CAUSE the intent falls under is deliberately not asked here. A turn is
// told more than one thing while it runs, and the record resolves the cause in
// force inside the same transaction as the insert; a value carried through here
// would be a snapshot taken earlier than the row it lands on.
type Running interface {
	RunningTurn() (ref string)
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
	// Resting names the underlyings this account already has an order working on.
	// A nil one leaves the list as the screener priced it - which is what a
	// deployment with no broker can honestly do.
	Resting Resting
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
	// Placements scores where the legs of a ratio structure should go. Nil means
	// no market is wired here and the tool is not offered - the same rule the rest
	// of these follow.
	Placements Placements
}

// Placements is the scorer, kept as an interface so the tool can be exercised
// without a broker.
type Placements interface {
	Score(ctx context.Context, ask placement.Ask) (placement.Answer, error)
}

// Asked answers whether a tool was called during one turn.
type Asked interface {
	AskedInTurn(ctx context.Context, turnRef, tool string) (bool, error)
	// TriedInTurn is the same question without "and it answered". A close is let
	// through an envelope that could not answer, never through one nobody called.
	TriedInTurn(ctx context.Context, turnRef, tool string) (bool, error)
}

// envelopeTool is the name a session calls to learn its limits. Named here
// because the check below is about that call and no other.
const envelopeTool = "read_envelope"

// Resting answers which underlyings already have an order of ours working on
// them. A structure on one of those is not a candidate: the session cannot tell
// its own resting order from a stranger's, would size against a position it does
// not yet hold, and the broker refuses the pair - measured twice on 28 August.
type Resting interface {
	RestingUnderlyings(ctx context.Context) ([]string, error)
}

// Shortlist is the last sweep of the universe, richest first.
type Shortlist interface {
	Candidates(ctx context.Context, most int) ([]screener.Candidate, time.Time, error)
}

// running names the turn an intent belongs to.
func (t Tools) running() string {
	if t.Running == nil {
		return ""
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
			// Refused HERE rather than by the column it lands in. The record stores
			// a number, so "MU p=939.15" fails at the database with a message about
			// a type, and the session is left guessing which of its fields was
			// wrong. Measured 28 August: five intents lost at once to exactly that.
			price, err := strconv.ParseFloat(strings.TrimSpace(in.UnderlyingPrice), 64)
			if err != nil {
				return nil, recordIntentOutput{}, fmt.Errorf(
					"underlying_price must be a bare number like 939.15, not %q: no ticker, no currency sign, no words", in.UnderlyingPrice)
			}
			// And a number the COLUMN can hold. It is NUMERIC(14,6): eight digits
			// before the point, six after. `1e100` parses as a float and is
			// refused by the database, which loses the intent the same way an
			// empty string did - the check that is here precisely so that a
			// session is told which field was wrong instead of meeting a message
			// about a type.
			if math.IsNaN(price) || math.IsInf(price, 0) || math.Abs(price) >= 1e8 {
				return nil, recordIntentOutput{}, fmt.Errorf(
					"underlying_price is %q: state what the underlying costs, as a number below 100000000", in.UnderlyingPrice)
			}
			at := now()
			turn := t.running()

			// What the session says it is answering, checked against what it was
			// actually given. Resolved to the row rather than stored as a name: two
			// wakings of the same session in one turn are two different moments, and
			// the later one is what a claim made now refers to.
			var answers *int64
			if in.Answers != "" {
				causes, err := state.CausesOfTurn(ctx, turn)
				if err != nil {
					return nil, recordIntentOutput{}, err
				}
				names := make([]string, 0, len(causes))
				for i := range causes {
					names = append(names, causes[i].WokenBy)
					if causes[i].WokenBy == in.Answers {
						id := causes[i].ID
						answers = &id
					}
				}
				if answers == nil {
					return nil, recordIntentOutput{}, fmt.Errorf(
						"nothing called %q was put in front of this turn; it was given: %s",
						in.Answers, strings.Join(names, ", "))
				}
			}

			// The limits have to have been read in THIS turn. Sessions are turns on
			// one conversation, so an answer from an earlier turn is still in the
			// model's context and does not read as stale to it - which is why this
			// is checked rather than asked for. A session that states an intent
			// from memory is stating it against limits that may have moved.
			// Whether this intent was checked, recorded beside it. A deployment
			// that cannot make the check records intents anyway - refusing every
			// one is worse - and a reader is entitled to know which of the two
			// kinds of row they are looking at.
			// A close does not wait for the limits. The envelope says how much may
			// be RISKED, and leaving a position risks nothing it was not already
			// risking; a session that cannot record an intent cannot order at all,
			// by its own rule, so an unreadable envelope would hold a position open.
			// Measured 1 September: the envelope was unreadable for one pass on both
			// accounts, and a position needing to leave in that window could not
			// have. The row says it was not checked and says it was a close.
			// False until the check is actually made. A deployment that cannot make
			// it, and a turn this tool cannot name, both leave the same absence, and
			// a row saying the envelope was read when nobody looked is worse than a
			// row saying nothing was checked.
			checked := false
			if t.Asked != nil && turn != "" {
				asked, err := t.Asked.AskedInTurn(ctx, turn, envelopeTool)
				if err != nil {
					return nil, recordIntentOutput{}, err
				}
				if !asked && !in.Closing {
					return nil, recordIntentOutput{}, fmt.Errorf(
						"call %s in this turn before recording an intent: limits change while a conversation runs, and an answer from an earlier turn is not this turn's answer",
						envelopeTool)
				}
				// A close is let through an envelope that could not ANSWER, never
				// through one nobody called. Without this the flag is a way to skip
				// the step rather than a way past a service that is down, and the
				// difference is invisible in the record afterwards.
				if !asked && in.Closing {
					tried, err := t.Asked.TriedInTurn(ctx, turn, envelopeTool)
					if err != nil {
						return nil, recordIntentOutput{}, err
					}
					if !tried {
						return nil, recordIntentOutput{}, fmt.Errorf(
							"call %s in this turn even to close: a close is excused an envelope that cannot answer, not one nobody asked",
							envelopeTool)
					}
				}
				// An envelope that DID answer was read, whichever way this intent
				// goes, and the row should not say otherwise.
				checked = asked
			}

			// The same structure stated twice in one turn is one decision written
			// down twice, and a judge reads it as two. The session is told, so it
			// orders rather than restating. A close is exempt: opening a structure
			// and then leaving it in the same turn is two decisions about one
			// structure, and refusing the second refuses the exit.
			if turn != "" && !in.Closing {
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
				Answers:   answers,
				Thesis:    in.Thesis,
				Structure: in.Structure,
				MaxLoss:   in.MaxLoss,

				UnderlyingPrice: in.UnderlyingPrice,
				EnvelopeChecked: &checked,
				IsClosing:       in.Closing,
			}); err != nil {
				return nil, recordIntentOutput{}, err
			}
			return nil, recordIntentOutput{RecordedAt: at, TurnRef: turn}, nil
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
					"Each structure carries the two contract symbols it is built from. Send those verbatim in the order and never build a symbol yourself: the strike in an option symbol is eight digits of the strike times a thousand, and getting that wrong names a contract that does not exist. Measured 28 August - a session wrote MU260828P00092500 for strike 922.5, the broker answered asset not found, and the best structure of that window was lost. " +
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
				found, withheld, err := t.withoutWhatIsAlreadyWorking(ctx, found)
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
					Withheld:   withheld,
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

	if t.Placements != nil {
		mcp.AddTool(server,
			&mcp.Tool{
				Name: "score_placements",
				Description: "Ask where to put the legs of a structure whose worst case sits in the MIDDLE - a backspread, and anything else that sells one and buys several. " +
					"Answers with every placement your own limits allow, priced at the sides of the book an order would cross, each replayed against this underlying's own history in weather like today's: " +
					"what it is expected to make, the median, the worst, how often it ends in the red, where the valley sits in sigmas, and how much of the expectation comes from the best one percent of history. " +
					"That last number is the one that cannot be seen by eye: a structure drawing most of its expectation from one percent of windows is a lottery ticket, not a cheap bet. " +
					"It ranks and it reports. It chooses nothing and sends nothing. The screener answers the other half - WHICH underlying, across all of them; this answers where inside one.",
			},
			func(ctx context.Context, req *mcp.CallToolRequest, in scorePlacementsInput) (*mcp.CallToolResult, placement.Answer, error) {
				expires, err := time.Parse(time.DateOnly, in.Expiration)
				if err != nil {
					return nil, placement.Answer{}, fmt.Errorf("expiration %q is not a date like 2026-09-04: %w", in.Expiration, err)
				}
				// Refused rather than defaulted. These are the declaration's numbers,
				// and a session that did not bring them is a session that did not read
				// its limits - which is exactly the case this project exists to make
				// impossible.
				if in.ShortLeastSigma <= 0 || in.ValleyLeastSigma <= 0 {
					return nil, placement.Answer{}, fmt.Errorf(
						"short_least_sigma and valley_least_sigma are required and come from YOUR declaration, not from here")
				}
				bought := in.Bought
				if bought == 0 {
					bought = 2
				}
				most := in.Most
				if most == 0 {
					most = 5
				}

				answer, err := t.Placements.Score(ctx, placement.Ask{
					Underlying:       strings.ToUpper(in.Underlying),
					Expiration:       expires,
					Kind:             strings.ToLower(in.Kind),
					Bought:           bought,
					ShortLeastSigma:  in.ShortLeastSigma,
					ValleyLeastSigma: in.ValleyLeastSigma,
					ShortMostSigma:   in.ShortMostSigma,
					WorstCaseMost:    in.WorstCaseMost,
					Most:             most,
				})
				if err != nil {
					return nil, placement.Answer{}, err
				}

				return nil, answer, nil
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
			func(ctx context.Context, req *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, wakeupsOutput, error) {
				return nil, wakeupsOutput{Wakeups: wakeups.List()}, nil
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
	// Withheld names the underlyings taken out of the list because an order of
	// ours is already working on them. Named rather than silently dropped: a
	// session that asked for ten and got seven is entitled to know whether the
	// market was thin or we were already in.
	Withheld []string `json:"withheld,omitempty" jsonschema:"underlyings left out because this account already has an order working on them"`
}

// withoutWhatIsAlreadyWorking drops the structures whose underlying already has
// an order of ours in the book, and names what it dropped.
//
// Two of them went to the broker on 28 August and came back refused. The session
// cannot see its own resting orders in this list, so it sizes a second position
// against an account that does not yet hold the first - and even where the
// broker accepts, the two orders are one bet taken twice.
func (t Tools) withoutWhatIsAlreadyWorking(ctx context.Context, found []screener.Candidate) ([]screener.Candidate, []string, error) {
	if t.Resting == nil || len(found) == 0 {
		return found, nil, nil
	}
	working, err := t.Resting.RestingUnderlyings(ctx)
	if err != nil {
		return nil, nil, err
	}
	if len(working) == 0 {
		return found, nil, nil
	}

	busy := make(map[string]bool, len(working))
	for _, underlying := range working {
		busy[strings.ToUpper(underlying)] = true
	}

	kept := make([]screener.Candidate, 0, len(found))
	var withheld []string
	seen := map[string]bool{}
	for _, candidate := range found {
		name := strings.ToUpper(candidate.Underlying)
		if !busy[name] {
			kept = append(kept, candidate)

			continue
		}
		if !seen[name] {
			seen[name] = true
			withheld = append(withheld, name)
		}
	}

	return kept, withheld, nil
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
