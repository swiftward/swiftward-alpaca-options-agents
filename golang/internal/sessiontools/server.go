// Package mcpserver is the MCP server this project runs for its own agent. It
// carries what the broker's server cannot: the intent a session states before it
// orders anything, and the state that same session reads when it wakes again.
//
// It never reaches the broker. Orders go through the policy gateway and the
// broker's own MCP server, which is what the hackathon requires.
package sessiontools

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/record"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/wakeup"
)

const (
	name    = "swiftward-alpaca-options-agents"
	version = "v0.1.0"
)

type recordIntentInput struct {
	Session   string `json:"session" jsonschema:"the session that is about to act, as the harness named it"`
	Thesis    string `json:"thesis" jsonschema:"why this trade, in one sentence"`
	Structure string `json:"structure" jsonschema:"the option structure about to be opened"`
	MaxLoss   string `json:"max_loss" jsonschema:"the largest loss this structure can produce"`
}

type recordIntentOutput struct {
	RecordedAt time.Time `json:"recorded_at" jsonschema:"when the intent was stored"`
}

type readStateInput struct{}

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

// Handler serves this server over Streamable HTTP. now is the clock the tools
// stamp with; it is passed in so a test is not at the mercy of the wall clock.
func Handler(state record.Keeper, now func() time.Time, poster Poster, wakeups Wakeups) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: name, Version: version}, nil)

	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "record_intent",
			Description: "State what this session is about to do and the loss it accepts, before sending any order.",
		},
		func(ctx context.Context, req *mcp.CallToolRequest, in recordIntentInput) (*mcp.CallToolResult, recordIntentOutput, error) {
			if in.Session == "" || in.Thesis == "" || in.Structure == "" || in.MaxLoss == "" {
				return nil, recordIntentOutput{}, fmt.Errorf("session, thesis, structure and max_loss are all required")
			}
			at := now()
			if err := state.AppendIntent(ctx, record.Intent{
				At:        at,
				Session:   in.Session,
				Thesis:    in.Thesis,
				Structure: in.Structure,
				MaxLoss:   in.MaxLoss,
			}); err != nil {
				return nil, recordIntentOutput{}, err
			}
			return nil, recordIntentOutput{RecordedAt: at}, nil
		})

	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "read_state",
			Description: "Read the ruleset in force, the limits it declares, the intents recorded so far and the refusals returned.",
		},
		func(ctx context.Context, req *mcp.CallToolRequest, _ readStateInput) (*mcp.CallToolResult, record.State, error) {
			current, err := state.Read(ctx)
			if err != nil {
				return nil, record.State{}, err
			}
			return nil, current, nil
		})

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
