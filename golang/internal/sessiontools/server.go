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

// Handler serves this server over Streamable HTTP. now is the clock the tools
// stamp with; it is passed in so a test is not at the mercy of the wall clock.
func Handler(state *record.Memory, now func() time.Time, poster Poster) http.Handler {
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
			state.AppendIntent(record.Intent{
				At:        at,
				Session:   in.Session,
				Thesis:    in.Thesis,
				Structure: in.Structure,
				MaxLoss:   in.MaxLoss,
			})
			return nil, recordIntentOutput{RecordedAt: at}, nil
		})

	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "read_state",
			Description: "Read the ruleset in force, the limits it declares, the intents recorded so far and the refusals returned.",
		},
		func(ctx context.Context, req *mcp.CallToolRequest, _ readStateInput) (*mcp.CallToolResult, record.State, error) {
			return nil, state.Read(), nil
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

	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
}
