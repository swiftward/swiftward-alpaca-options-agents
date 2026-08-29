package sessiontools

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/record"
)

// The record keeps a call's arguments as raw JSON, which describes itself to the
// protocol as an array of bytes and then arrives as an object. Every read_state
// was refused on its own output schema, so defence and the end-of-day close lost
// the only way they had to recognise a position opened as a probe - and nothing
// said so except one line inside the session's own answer.
func TestArgumentsCrossToTheSessionAsText(t *testing.T) {
	answer := answerWith(record.State{
		Calls: []record.ToolCall{{
			Ref: "c-1", Tool: "get_option_snapshot", Status: "completed",
			Arguments: json.RawMessage(`{"feed":"indicative","limit":30}`),
		}},
	})

	require.Len(t, answer.Calls, 1)
	assert.Equal(t, `{"feed":"indicative","limit":30}`, answer.Calls[0].Arguments)

	written, err := json.Marshal(answer)
	require.NoError(t, err)

	var read struct {
		Calls []struct {
			Arguments json.RawMessage `json:"arguments"`
		} `json:"calls"`
	}
	require.NoError(t, json.Unmarshal(written, &read))
	assert.Equal(t, `"{\"feed\":\"indicative\",\"limit\":30}"`, string(read.Calls[0].Arguments),
		"a string, never an object: the schema the protocol checks says string")
}

func TestACallWithoutArgumentsCarriesNoText(t *testing.T) {
	answer := answerWith(record.State{
		Calls: []record.ToolCall{{Ref: "c-1", Tool: "get_clock", Status: "completed"}},
	})

	require.Len(t, answer.Calls, 1)
	assert.Empty(t, answer.Calls[0].Arguments)
}
