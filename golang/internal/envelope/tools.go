package envelope

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	name    = "swiftward-policy-gateway"
	version = "v0.1.0"
)

type readEnvelopeInput struct {
	Tool string `json:"tool" jsonschema:"the tool you are about to use, for example place_option_order"`
}

// Tools serves the envelope over Streamable HTTP.
//
// Callers maps a bearer token to the identity it belongs to. The session never
// sees this map and cannot influence which identity it is given: which limits
// apply is decided by whoever handed out the token, which is what makes them
// limits rather than suggestions.
type Tools struct {
	Path    string
	Callers map[string]string
}

// Handler serves read_envelope. The identity is resolved once per connection,
// from the bearer token the session was started with.
func (t Tools) Handler() http.Handler {
	return mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
		return t.server(identityOf(req, t.Callers))
	}, nil)
}

func (t Tools) server(identity string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: name, Version: version}, nil)

	mcp.AddTool(server,
		&mcp.Tool{
			Name: "read_envelope",
			Description: "Read what you are allowed to do on a tool before you use it: the limits in force for you right now " +
				"and the version of the ruleset that produced them. Reaches no broker and moves no money. " +
				"Ask before building any order, and again whenever the tool list changes.",
		},
		func(ctx context.Context, req *mcp.CallToolRequest, in readEnvelopeInput) (*mcp.CallToolResult, Envelope, error) {
			if strings.TrimSpace(in.Tool) == "" {
				return nil, Envelope{}, fmt.Errorf("tool is required: an envelope is the limits on one tool, not a list of everything")
			}
			if identity == "" {
				return nil, Envelope{}, fmt.Errorf("this caller is under no ruleset: do not trade, and say so")
			}

			// Read from the file on every call. An operator lowering a ceiling
			// mid-session is the behaviour this exists to show, and a value cached
			// at startup would show the opposite.
			set, err := Load(t.Path)
			if err != nil {
				return nil, Envelope{}, err
			}
			out, err := set.For(identity, in.Tool)
			if err != nil {
				return nil, Envelope{}, err
			}
			return nil, out, nil
		})

	return server
}

// identityOf reads the bearer token off the request and answers who it belongs
// to. An unknown or absent token resolves to nobody, and the tool then refuses
// to answer rather than answering "no limits".
func identityOf(req *http.Request, callers map[string]string) string {
	header := req.Header.Get("Authorization")
	token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer"))
	if token == "" {
		return ""
	}
	return callers[token]
}
