package sessiontools

import (
	"time"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/record"
)

// What the session is told when it reads the record.
//
// This is deliberately NOT the record's own types. The record stores a tool
// call's arguments as raw JSON, which describes itself to the protocol as an
// array of bytes and then arrives as an object - so every read_state was refused
// on its own output schema, and the sessions that depend on it (defence and the
// end-of-day close, which must not touch a position opened as a probe) silently
// lost the only way they had to recognise one.
//
// The arguments cross as the text the session sent, which is unambiguous to
// describe and is what a reader wants to see anyway.
type stateAnswer struct {
	Turns   []record.Turn          `json:"turns"`
	Calls   []toolCallAnswer       `json:"calls"`
	Steps   []record.ExecutionStep `json:"steps"`
	Intents []record.Intent        `json:"intents"`
}

type toolCallAnswer struct {
	Ref     string `json:"ref"`
	TurnRef string `json:"turn_ref"`
	Server  string `json:"server"`
	Tool    string `json:"tool"`
	// Arguments is the JSON the session sent, as text.
	Arguments  string     `json:"arguments,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Status     string     `json:"status"`
	Failure    string     `json:"failure,omitempty"`
	Answer     string     `json:"answer,omitempty"`
}

func answerWith(state record.State) stateAnswer {
	calls := make([]toolCallAnswer, 0, len(state.Calls))
	for _, call := range state.Calls {
		calls = append(calls, toolCallAnswer{
			Ref: call.Ref, TurnRef: call.TurnRef, Server: call.Server, Tool: call.Tool,
			Arguments: string(call.Arguments),
			StartedAt: call.StartedAt, FinishedAt: call.FinishedAt,
			Status: call.Status, Failure: call.Failure, Answer: call.Answer,
		})
	}

	return stateAnswer{
		Turns: state.Turns, Calls: calls, Steps: state.Steps, Intents: state.Intents,
	}
}
