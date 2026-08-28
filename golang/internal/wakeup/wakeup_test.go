package wakeup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openStore(t *testing.T) (*Store, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "state", "wakeups.json")
	store, err := Open(path)
	require.NoError(t, err)

	return store, path
}

var now = time.Date(2026, 8, 24, 14, 0, 0, 0, time.UTC)

func TestAWakeUpSurvivesTheProcess(t *testing.T) {
	store, path := openStore(t)

	_, err := store.AddAt("посмотреть, как открылась позиция", now.Add(time.Hour), now)
	require.NoError(t, err)
	_, err = store.AddPrice("цена подошла к проданному страйку", "spy", Below, 760, now)
	require.NoError(t, err)

	reopened, err := Open(path)
	require.NoError(t, err)
	list := reopened.List()

	require.Len(t, list, 2)
	assert.Equal(t, Cause("посмотреть, как открылась позиция"), list[0].Cause)
	assert.Equal(t, "SPY", list[1].Symbol, "the symbol is stored the way the broker spells it")
	assert.Equal(t, []string{"SPY"}, reopened.Watching())
}

// A wake-up without a reason wakes a session that cannot know what it meant.
func TestAWakeUpNeedsACause(t *testing.T) {
	store, _ := openStore(t)

	_, err := store.AddAt("  ", now.Add(time.Hour), now)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cause")

	_, err = store.AddPrice("", "SPY", Below, 760, now)
	require.Error(t, err)
}

func TestRefusesWhatCannotFire(t *testing.T) {
	store, _ := openStore(t)

	_, err := store.AddAt("уже прошло", now.Add(-time.Minute), now)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in the future")

	_, err = store.AddPrice("нет символа", "", Below, 760, now)
	require.Error(t, err)

	_, err = store.AddPrice("непонятная сторона", "SPY", Direction("sideways"), 760, now)
	require.Error(t, err)

	_, err = store.AddPrice("цена не цена", "SPY", Below, 0, now)
	require.Error(t, err)
}

func TestDueFiresOnceAndForgets(t *testing.T) {
	store, path := openStore(t)

	soon, err := store.AddAt("проверить позицию", now.Add(30*time.Minute), now)
	require.NoError(t, err)
	_, err = store.AddAt("конец дня", now.Add(4*time.Hour), now)
	require.NoError(t, err)

	assert.Empty(t, store.Due(now.Add(10*time.Minute), nil), "not yet")

	due := store.Due(now.Add(31*time.Minute), nil)
	require.Len(t, due, 1)
	assert.Equal(t, soon.ID, due[0].ID)
	assert.Contains(t, due[0].Prompt(), "проверить позицию")

	assert.Empty(t, store.Due(now.Add(32*time.Minute), nil), "a wake-up fires once")

	reopened, err := Open(path)
	require.NoError(t, err)
	assert.Len(t, reopened.List(), 1, "the fired one is not waiting after a restart either")
}

func TestPriceWakeUpsFireOnTheirSide(t *testing.T) {
	store, _ := openStore(t)

	_, err := store.AddPrice("цена ушла ниже страйка", "SPY", Below, 760, now)
	require.NoError(t, err)
	_, err = store.AddPrice("цена вернулась выше", "SPY", Above, 770, now)
	require.NoError(t, err)

	assert.Empty(t, store.Due(now, map[string]float64{"SPY": 765}), "between the levels")
	assert.Empty(t, store.Due(now, nil), "no price, no decision")

	due := store.Due(now, map[string]float64{"SPY": 759.5})
	require.Len(t, due, 1)
	assert.Contains(t, due[0].Prompt(), "below")

	due = store.Due(now, map[string]float64{"SPY": 771})
	require.Len(t, due, 1)
	assert.Contains(t, due[0].Prompt(), "above")
}

func TestCancelSaysWhenThereIsNothingToCancel(t *testing.T) {
	store, _ := openStore(t)

	created, err := store.AddAt("посмотреть позже", now.Add(time.Hour), now)
	require.NoError(t, err)

	require.NoError(t, store.Cancel(created.ID))
	assert.Empty(t, store.List())

	err = store.Cancel(created.ID)
	require.Error(t, err, "a session that thinks it cancelled will plan as if it did")
}

// Identifiers keep counting after a restart, so a cancelled id is never handed
// to a second wake-up the session might confuse it with.
func TestIdentifiersDoNotRepeatAfterARestart(t *testing.T) {
	store, path := openStore(t)

	first, err := store.AddAt("раз", now.Add(time.Hour), now)
	require.NoError(t, err)

	reopened, err := Open(path)
	require.NoError(t, err)
	second, err := reopened.AddAt("два", now.Add(2*time.Hour), now)
	require.NoError(t, err)

	assert.NotEqual(t, first.ID, second.ID)
}

func TestAMissingFileIsAnEmptyStore(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "nothing-here.json"))
	require.NoError(t, err)
	assert.Empty(t, store.List())
}

func TestAnUnreadableFileIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wakeups.json")
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o600))

	_, err := Open(path)
	require.Error(t, err)
}

// A price wake-up has no time: it waits for a number. Carrying `at` as the zero
// value told the session something false - `0001-01-01T00:00:00Z` reads as a
// moment, and sorting standing wake-ups by it puts every price one first, in the
// first year of our era.
func TestAPriceWakeupCarriesNoTime(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "wakeups.json"))
	require.NoError(t, err)

	now := time.Date(2026, 9, 4, 13, 30, 0, 0, time.UTC)

	priced, err := store.AddPrice("цена подошла к проданному страйку", "SPY", Below, 600, now)
	require.NoError(t, err)
	timed, err := store.AddAt("посмотреть, как открылась позиция", now.Add(time.Hour), now)
	require.NoError(t, err)

	byPrice, err := json.Marshal(priced)
	require.NoError(t, err)
	assert.NotContains(t, string(byPrice), "0001-01-01")
	assert.NotContains(t, string(byPrice), `"at"`, "у ценового пробуждения времени нет вовсе")
	assert.Contains(t, string(byPrice), `"level":600`)

	byTime, err := json.Marshal(timed)
	require.NoError(t, err)
	assert.Contains(t, string(byTime), `"at":"2026-09-04T14:30:00Z"`,
		"а у временного оно обязано остаться на месте")

	// И то, что записано на диск, читается обратно тем же.
	reopened, err := Open(store.path)
	require.NoError(t, err)
	assert.Len(t, reopened.List(), 2)
}
