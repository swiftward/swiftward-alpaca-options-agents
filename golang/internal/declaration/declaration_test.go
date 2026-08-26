package declaration

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func write(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "agent.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	return path
}

const good = `
kind: trading-agent
name: options-alpha
version: v1
timezone: America/New_York
sessions:
  - name: entry
    cause: "окно входа во второй половине сессии"
    task: "Оцени условия входа и открой позицию, если они сошлись."
    at: "14:20"
    within: 45m
    days: [mon, tue, wed, thu, fri]
  - name: defend
    cause: "проверка правил защиты"
    task: "Проверь открытые позиции против правил защиты."
    every: 30m
    between: ["09:40", "15:55"]
`

func TestLoadReadsSessions(t *testing.T) {
	d, err := Load(write(t, good))
	require.NoError(t, err)

	assert.Equal(t, "options-alpha", d.Name)
	require.Len(t, d.Sessions, 2)
	assert.Equal(t, "entry", d.Sessions[0].Name)
	assert.Contains(t, d.Sessions[0].Prompt(), "окно входа")
	assert.Contains(t, d.Sessions[0].Prompt(), "открой позицию")
	assert.Equal(t, "America/New_York", d.Location().String())
}

// Every rejection names the session it belongs to: a schedule that silently drops
// one is worse than one that refuses to load.
func TestLoadRefusesWhatCannotRun(t *testing.T) {
	cases := map[string]struct {
		body string
		want string
	}{
		"wrong_kind":        {body: "kind: something\nname: a\n", want: "expected"},
		"no_sessions":       {body: "kind: trading-agent\nname: a\ntimezone: UTC\n", want: "wakes nobody"},
		"no_timezone":       {body: "kind: trading-agent\nname: a\nsessions: [{name: s, cause: c, task: t, at: \"10:00\"}]\n", want: "timezone"},
		"unknown_timezone":  {body: "kind: trading-agent\nname: a\ntimezone: Mars/Olympus\nsessions: [{name: s, cause: c, task: t, at: \"10:00\"}]\n", want: "Mars/Olympus"},
		"no_cause":          {body: "kind: trading-agent\nname: a\ntimezone: UTC\nsessions: [{name: s, task: t, at: \"10:00\", within: 1h}]\n", want: "no cause"},
		"no_within":         {body: "kind: trading-agent\nname: a\ntimezone: UTC\nsessions: [{name: s, cause: c, task: t, at: \"10:00\"}]\n", want: "how late it may still start"},
		"no_task":           {body: "kind: trading-agent\nname: a\ntimezone: UTC\nsessions: [{name: s, cause: c, at: \"10:00\", within: 1h}]\n", want: "no task"},
		"both_at_and_every": {body: "kind: trading-agent\nname: a\ntimezone: UTC\nsessions: [{name: s, cause: c, task: t, at: \"10:00\", within: 1h, every: 30m, between: [\"09:00\",\"10:00\"]}]\n", want: "one way to wake"},
		"neither":           {body: "kind: trading-agent\nname: a\ntimezone: UTC\nsessions: [{name: s, cause: c, task: t}]\n", want: "nothing would wake it"},
		"every_too_small":   {body: "kind: trading-agent\nname: a\ntimezone: UTC\nsessions: [{name: s, cause: c, task: t, every: 5s, between: [\"09:00\",\"10:00\"]}]\n", want: "takes longer than that"},
		"window_backwards":  {body: "kind: trading-agent\nname: a\ntimezone: UTC\nsessions: [{name: s, cause: c, task: t, every: 30m, between: [\"15:00\",\"09:00\"]}]\n", want: "ends before it starts"},
		"bad_day":           {body: "kind: trading-agent\nname: a\ntimezone: UTC\nsessions: [{name: s, cause: c, task: t, at: \"10:00\", within: 1h, days: [funday]}]\n", want: "not a weekday"},
		"unknown_field":     {body: "kind: trading-agent\nname: a\ntimezone: UTC\nprofit: yes\nsessions: [{name: s, cause: c, task: t, at: \"10:00\", within: 1h}]\n", want: "field profit"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Load(write(t, tc.body))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func at(t *testing.T, value string) time.Time {
	t.Helper()

	parsed, err := time.Parse("2006-01-02 15:04", value)
	require.NoError(t, err)

	return parsed
}

func TestDueOnceADay(t *testing.T) {
	d, err := Load(write(t, good))
	require.NoError(t, err)
	entry := &d.Sessions[0]

	monday := at(t, "2026-08-24 14:20")
	assert.True(t, entry.Due(monday, time.Time{}), "never run and the hour has come")
	assert.False(t, entry.Due(at(t, "2026-08-24 14:19"), time.Time{}), "the hour has not come")
	assert.False(t, entry.Due(monday, monday), "already run today")
	assert.True(t, entry.Due(at(t, "2026-08-25 14:20"), monday), "a new day")
	assert.False(t, entry.Due(at(t, "2026-08-29 14:20"), monday), "saturday is not a trading day")

	// A process that starts late still runs today's session while the window it
	// serves is open, and not after it closed.
	assert.True(t, entry.Due(at(t, "2026-08-24 15:00"), time.Time{}), "forty minutes late is still the entry window")
	assert.False(t, entry.Due(at(t, "2026-08-24 18:00"), time.Time{}), "hours late is a different day's decision")
}

func TestDueEveryInsideItsWindow(t *testing.T) {
	d, err := Load(write(t, good))
	require.NoError(t, err)
	defend := &d.Sessions[1]

	start := at(t, "2026-08-24 09:40")
	assert.True(t, defend.Due(start, time.Time{}))
	assert.False(t, defend.Due(at(t, "2026-08-24 09:39"), time.Time{}), "before the window")
	assert.False(t, defend.Due(at(t, "2026-08-24 16:00"), time.Time{}), "after the window")
	assert.False(t, defend.Due(at(t, "2026-08-24 10:00"), start), "twenty minutes is not thirty")
	assert.True(t, defend.Due(at(t, "2026-08-24 10:10"), start), "thirty minutes have passed")
}

// EVERY declaration this project ships must load, and every session in it must
// carry a cause and a task. A declaration that only one agent reads is still one
// the week depends on, and a broken one is discovered when that agent wakes
// nobody all day.
func TestEveryDeclarationWeShipLoads(t *testing.T) {
	// Run this with -count=1. Go caches a test result against the Go files it
	// compiled, and these declarations are not among them: edit a YAML, run the
	// test, and it answers "ok (cached)" about the file as it was. On 26 August it
	// did exactly that while the declaration had a syntax error, and the agent
	// crash-looped on startup. `make test` passes -count=1 for this reason.

	// The real files, not copies: a copy would drift from what ships.
	shipped, err := filepath.Glob("../../../agent/*.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, shipped, "no declarations found: the path this test reads has moved")

	for _, path := range shipped {
		t.Run(filepath.Base(path), func(t *testing.T) {
			d, err := Load(path)
			require.NoError(t, err)

			assert.Equal(t, "America/New_York", d.Location().String())
			assert.NotEmpty(t, d.Name)
			names := map[string]bool{}
			for i := range d.Sessions {
				s := &d.Sessions[i]
				assert.False(t, names[s.Name], "two sessions named %s", s.Name)
				names[s.Name] = true
				assert.NotEmpty(t, s.Cause, s.Name)
				assert.NotEmpty(t, s.Task, s.Name)
			}
			// Whatever else a declaration carries, it has to be able to open a
			// position, defend one, and end the day.
			for _, want := range []string{"entry", "defend", "flatten"} {
				assert.True(t, names[want], "session %s is missing", want)
			}
		})
	}
}

// A session that cannot read its own schedule answers "nothing will wake me" to
// the person who wrote the schedule. This is what it reads instead.
func TestTheScheduleReadsAsSentences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
kind: trading-agent
name: options-alpha
timezone: America/New_York
sessions:
  - name: entry
    cause: "окно входа"
    task: "Проверь вход."
    at: "14:20"
    within: 45m
    days: [mon, tue, wed, thu, fri]
  - name: news-watch
    cause: "новости"
    task: "Прочитай ленту."
    every: 30m
    between: ["09:35", "15:45"]
    model: gpt-5.6-luna
`), 0o600))

	declared, err := Load(path)
	require.NoError(t, err)

	schedule := declared.Schedule()

	require.Len(t, schedule, 2)
	assert.Equal(t, "entry", schedule[0].Name)
	assert.Equal(t, "at 14:20, still valid for 45m after that, on mon, tue, wed, thu, fri (America/New_York)", schedule[0].When)
	assert.Equal(t, "every 30m between 09:35 and 15:45 (America/New_York)", schedule[1].When)
	assert.Equal(t, "gpt-5.6-luna", schedule[1].Model)
}
