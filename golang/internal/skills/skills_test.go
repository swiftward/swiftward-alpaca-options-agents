package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// put writes one skill the way the repository holds it: a directory with a
// SKILL.md that opens with front matter.
func put(t *testing.T, source, dir, front, body string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Join(source, dir), 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(source, dir, "SKILL.md"),
		[]byte("---\n"+front+"\n---\n\n"+body+"\n"), 0o600))
}

func TestReadTakesTheNameFromTheFrontMatter(t *testing.T) {
	source := t.TempDir()
	put(t, source, "harvest", "name: playbook-premium-harvest\nrequires: [short_leg_delta]", "Sell a spread.")
	put(t, source, "envelope", "name: read-my-envelope", "Ask first.")
	// A directory without a manifest is not a skill and must not stop the start:
	// notes and scratch files live beside skills.
	require.NoError(t, os.MkdirAll(filepath.Join(source, "notes"), 0o755))

	found, err := Read(source)
	require.NoError(t, err)

	require.Len(t, found, 2)
	assert.Equal(t, "playbook-premium-harvest", found[0].Name)
	assert.Equal(t, []string{"short_leg_delta"}, found[0].Requires)
	assert.Equal(t, "read-my-envelope", found[1].Name)
	assert.Nil(t, found[1].Requires)
}

// Carrying no skills is a state this project is allowed to be in: git keeps no
// empty directory, so deleting the last skill deletes the tree.
func TestNoSkillsAtAllIsNotAFailure(t *testing.T) {
	found, err := Read(filepath.Join(t.TempDir(), "never-created"))
	require.NoError(t, err)
	assert.Empty(t, found)
}

func TestReadRefusesAManifestItCannotTrust(t *testing.T) {
	cases := map[string]struct{ front, want string }{
		"no_name": {front: "description: a thing", want: "names no skill"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			source := t.TempDir()
			put(t, source, "one", tc.front, "body")
			_, err := Read(source)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}

	t.Run("no_front_matter", func(t *testing.T) {
		source := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(source, "one"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(source, "one", "SKILL.md"),
			[]byte("# Just a heading\n"), 0o600))
		_, err := Read(source)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "front matter")
	})

	t.Run("two_of_one_name", func(t *testing.T) {
		source := t.TempDir()
		put(t, source, "a", "name: same", "body")
		put(t, source, "b", "name: same", "body")
		_, err := Read(source)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `call themselves "same"`)
	})
}

func TestLayPutsOnlyWhatTheDeclarationNamed(t *testing.T) {
	source, work := t.TempDir(), t.TempDir()
	put(t, source, "harvest", "name: playbook-premium-harvest\nrequires: [short_leg_delta]", "Sell a spread.")
	put(t, source, "envelope", "name: read-my-envelope", "Ask first.")
	put(t, source, "backspread", "name: playbook-backspread", "Buy convexity.")

	layer := &Layer{Source: source, AgentDir: work}
	laid, err := layer.Lay(
		[]string{"playbook-premium-harvest", "read-my-envelope"},
		map[string]string{"short_leg_delta": "0,15"})
	require.NoError(t, err)

	assert.True(t, laid.Changed)
	assert.Equal(t, filepath.Join(work, ".agents", "skills"), laid.Dir)
	assert.Equal(t, []string{"playbook-premium-harvest", "read-my-envelope"}, laid.Names)

	body, err := os.ReadFile(filepath.Join(laid.Dir, "harvest", "SKILL.md"))
	require.NoError(t, err)
	assert.Contains(t, string(body), "Sell a spread.")

	// The one nobody asked for costs its description in every prompt of every
	// turn, which is the whole reason the set is narrowed. This holds whether the
	// source is the image's own or a checkout mounted over it - the narrowing is
	// done here, by copying, and not by what happens to be on disk.
	_, err = os.Stat(filepath.Join(laid.Dir, "backspread"))
	assert.True(t, os.IsNotExist(err), "a skill this agent was not given must not be there")
}

// Empty means all of them: a declaration written before this key existed keeps
// the behaviour it had, and so does a deployment with no declaration at all.
func TestNoChoiceMeansEverySkill(t *testing.T) {
	source, work := t.TempDir(), t.TempDir()
	put(t, source, "harvest", "name: playbook-premium-harvest", "Sell a spread.")
	put(t, source, "envelope", "name: read-my-envelope", "Ask first.")

	layer := &Layer{Source: source, AgentDir: work}
	laid, err := layer.Lay(nil, nil)
	require.NoError(t, err)

	assert.Equal(t, []string{"playbook-premium-harvest", "read-my-envelope"}, laid.Names)
	for _, dir := range []string{"harvest", "envelope"} {
		_, err := os.Stat(filepath.Join(laid.Dir, dir, "SKILL.md"))
		assert.NoError(t, err)
	}
}

// The directory outlives the image, so a skill deleted upstream has to disappear
// from the session rather than linger as an instruction nothing carries.
func TestLayClearsWhatAnEarlierStartLeft(t *testing.T) {
	source, work := t.TempDir(), t.TempDir()
	put(t, source, "harvest", "name: playbook-premium-harvest", "Sell a spread.")

	target := filepath.Join(work, ".agents", "skills")
	stale := filepath.Join(target, "retired")
	require.NoError(t, os.MkdirAll(stale, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(stale, "SKILL.md"), []byte("---\nname: retired\n---\n"), 0o600))

	layer := &Layer{Source: source, AgentDir: work}
	_, err := layer.Lay(nil, nil)
	require.NoError(t, err)

	_, err = os.Stat(stale)
	assert.True(t, os.IsNotExist(err), "a skill this image no longer carries must be gone")
}

// The reason the whole thing is asked on every tick: a session opens SKILL.md
// while it works, so editing the text of a technique has to reach a session that
// is already running.
func TestAnEditedSkillReachesTheDirectoryOnTheNextPass(t *testing.T) {
	source, work := t.TempDir(), t.TempDir()
	put(t, source, "harvest", "name: playbook-premium-harvest", "sell at fifteen delta")

	layer := &Layer{Source: source, AgentDir: work}
	first, err := layer.Lay([]string{"playbook-premium-harvest"}, nil)
	require.NoError(t, err)
	require.True(t, first.Changed)

	manifest := filepath.Join(first.Dir, "harvest", "SKILL.md")
	body, err := os.ReadFile(manifest)
	require.NoError(t, err)
	require.Contains(t, string(body), "sell at fifteen delta")

	// Nothing moved: the pass must not rewrite the directory a session is reading
	// once a minute for nothing.
	again, err := layer.Lay([]string{"playbook-premium-harvest"}, nil)
	require.NoError(t, err)
	assert.False(t, again.Changed)

	put(t, source, "harvest", "name: playbook-premium-harvest", "sell at thirty delta")

	changed, err := layer.Lay([]string{"playbook-premium-harvest"}, nil)
	require.NoError(t, err)
	assert.True(t, changed.Changed)

	body, err = os.ReadFile(manifest)
	require.NoError(t, err)
	assert.Contains(t, string(body), "sell at thirty delta", "the edited text has to reach the session")
}

// The choice and the numbers are part of what the session ends up reading, so a
// declaration that changes either rebuilds the directory even with the source
// untouched.
func TestANarrowedChoiceRebuildsTheDirectory(t *testing.T) {
	source, work := t.TempDir(), t.TempDir()
	put(t, source, "harvest", "name: playbook-premium-harvest", "Sell a spread.")
	put(t, source, "envelope", "name: read-my-envelope", "Ask first.")

	layer := &Layer{Source: source, AgentDir: work}
	both, err := layer.Lay(nil, nil)
	require.NoError(t, err)
	require.Len(t, both.Names, 2)

	one, err := layer.Lay([]string{"playbook-premium-harvest"}, nil)
	require.NoError(t, err)
	assert.True(t, one.Changed)
	assert.Equal(t, []string{"playbook-premium-harvest"}, one.Names)

	_, err = os.Stat(filepath.Join(one.Dir, "envelope"))
	assert.True(t, os.IsNotExist(err), "a skill the declaration stopped naming must go")
}

func TestLayRefusesASkillThatIsNotThere(t *testing.T) {
	source, work := t.TempDir(), t.TempDir()
	put(t, source, "harvest", "name: playbook-premium-harvest", "Sell a spread.")

	layer := &Layer{Source: source, AgentDir: work}
	_, err := layer.Lay(nil, nil)
	require.NoError(t, err)

	_, err = layer.Lay([]string{"playbook-defence"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no skill called "playbook-defence"`)
	assert.Contains(t, err.Error(), `"playbook-premium-harvest"`, "the error says what is actually there")

	// Nothing was removed: the check runs before the first write, so a misspelled
	// name leaves the session with the skills it had rather than with none.
	_, statErr := os.Stat(filepath.Join(work, ".agents", "skills", "harvest", "SKILL.md"))
	assert.NoError(t, statErr)
}

// A skill that quietly falls back on its own example number is how two accounts
// running the same playbook become one account run twice.
func TestLayRefusesWhenAParameterIsMissing(t *testing.T) {
	source, work := t.TempDir(), t.TempDir()
	put(t, source, "harvest",
		"name: playbook-premium-harvest\nrequires: [short_leg_delta, min_edge_points]", "Sell a spread.")

	layer := &Layer{Source: source, AgentDir: work}
	_, err := layer.Lay(nil, map[string]string{"short_leg_delta": "0,15"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"playbook-premium-harvest"`)
	assert.Contains(t, err.Error(), `"min_edge_points"`)
}

// Every skill the session can reach has to have its numbers, and with the set
// narrowed that is the chosen ones. A skill left out of `skills:` is not
// reachable and its requirement is not the declaration's to satisfy.
func TestASkillNobodyNamedNeedsNothing(t *testing.T) {
	source, work := t.TempDir(), t.TempDir()
	put(t, source, "harvest", "name: playbook-premium-harvest", "Sell a spread.")
	put(t, source, "draft", "name: playbook-draft\nrequires: [wing_width]", "written this morning")

	layer := &Layer{Source: source, AgentDir: work}
	laid, err := layer.Lay([]string{"playbook-premium-harvest"}, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"playbook-premium-harvest"}, laid.Names)

	// And with nothing narrowing it, the same skill IS reachable and its number is
	// required.
	_, err = layer.Lay(nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"wing_width"`)
}

func TestCheckRefusesASkillNamedTwice(t *testing.T) {
	source := t.TempDir()
	put(t, source, "harvest", "name: playbook-premium-harvest", "Sell a spread.")

	_, err := Check(source, []string{"playbook-premium-harvest", "playbook-premium-harvest"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "named twice")
}

// Mounting over the directory the session reads is refused rather than handled.
// Handled, the session would read every skill in the checkout instead of the
// ones the declaration named, and the same declaration would behave one way on a
// developer machine and another on a deployment.
func TestAMountedTargetIsRefusedRatherThanRebuilt(t *testing.T) {
	source, work := t.TempDir(), t.TempDir()
	put(t, source, "harvest", "name: playbook-premium-harvest", "the version inside the image")

	target := filepath.Join(work, ".agents", "skills")
	put(t, target, "harvest", "name: playbook-premium-harvest", "the version being edited")

	mountsInTree = func(_, path string) (bool, error) { return path == target, nil }
	t.Cleanup(func() { mountsInTree = mountedInTree })

	layer := &Layer{Source: source, AgentDir: work}
	_, err := layer.Lay([]string{"playbook-premium-harvest"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SKILLS_DIR", "the refusal says where to mount instead")

	// And nothing of the operator's was touched on the way to refusing.
	body, err := os.ReadFile(filepath.Join(target, "harvest", "SKILL.md"))
	require.NoError(t, err)
	assert.Contains(t, string(body), "the version being edited")
}

// A bind mount within one filesystem keeps the device of its parent, so the
// device comparison would answer "not mounted" about a directory that is. The
// mount table names mount points exactly, which is why it is read instead.
//
// Three shapes count, and they count for the same reason: the files there are
// not this process's to delete. Mounted AT the directory is the one somebody
// reaches for first; mounted BENEATH it is one skill being edited on its own,
// and rebuilding the tree around it would fail on a busy mount point; mounted
// ABOVE it, but below the directory this process works in, is the dangerous one
// - the tree we would rebuild lives inside somebody else's mount.
func TestTheMountTableIsReadForAnythingInTheTree(t *testing.T) {
	const root = "/work"

	line := func(where string) string {
		return "32 31 0:27 /agent/skills " + where + " ro,relatime - ext4 /dev/sda rw"
	}
	// The work directory is itself a mount - it is the volume this process is
	// given - and that must not read as somebody else's.
	volume := "31 30 0:26 / /work rw,relatime - ext4 /dev/sdb rw"

	cases := map[string]struct {
		table string
		want  bool
	}{
		"nothing mounted in the tree": {
			table: volume, want: false,
		},
		"mounted at the directory": {
			table: volume + "\n" + line("/work/.agents/skills"), want: true,
		},
		"one skill mounted on its own": {
			table: volume + "\n" + line("/work/.agents/skills/playbook-premium-harvest"), want: true,
		},
		"mounted above the directory": {
			table: volume + "\n" + line("/work/.agents"), want: true,
		},
		"a neighbour that only shares a prefix": {
			table: volume + "\n" + line("/work/.agents/skills-draft"), want: false,
		},
		// The source the skills are COPIED from is mounted on every developer
		// machine, and it is nowhere near the tree this rebuilds.
		"the source, which is where a checkout belongs": {
			table: volume + "\n" + line("/mnt/skills"), want: false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := mountedInTreeFrom(strings.NewReader(tc.table), root, "/work/.agents/skills")
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// A path with a space in it is two fields unless the octal escapes mountinfo
// writes are read back.
func TestAMountPointWithASpaceIsStillOneMountPoint(t *testing.T) {
	table := `33 30 0:28 / /work/two\040words rw,relatime - ext4 /dev/sdc rw`

	got, err := mountedInTreeFrom(strings.NewReader(table), "/work", "/work/two words")
	require.NoError(t, err)
	assert.True(t, got)
}

// Where there is no mount table - a developer's machine, this test - nothing is
// mounted as far as this can tell, which is the answer that writes only into
// what this process itself put there.
func TestNoMountTableMeansNothingIsMounted(t *testing.T) {
	work := t.TempDir()

	mounted, err := mountedInTree(work, filepath.Join(work, ".agents", "skills"))
	require.NoError(t, err)
	assert.False(t, mounted)
}

// The half-built set must not survive a failure pretending to be a skill nobody
// chose: the directory it is built in sits beside the one in use.
func TestAFailedPassLeavesNothingHalfBuilt(t *testing.T) {
	source, work := t.TempDir(), t.TempDir()
	put(t, source, "harvest", "name: playbook-premium-harvest", "Sell a spread.")

	layer := &Layer{Source: source, AgentDir: work}
	_, err := layer.Lay(nil, nil)
	require.NoError(t, err)

	for _, leftover := range []string{"skills.next", "skills.old"} {
		_, err = os.Stat(filepath.Join(work, ".agents", leftover))
		assert.True(t, os.IsNotExist(err), "%s is not left behind", leftover)
	}
}

// The session works in this directory and can write there. A comparison against
// what this process remembers laying would let a damaged copy stand for the rest
// of the day while every tick reported nothing to do, so the comparison is
// against what is on disk.
func TestADamagedDirectoryIsRepairedOnTheNextPass(t *testing.T) {
	source, work := t.TempDir(), t.TempDir()
	put(t, source, "harvest", "name: playbook-premium-harvest", "sell at fifteen delta")

	layer := &Layer{Source: source, AgentDir: work}
	laid, err := layer.Lay(nil, nil)
	require.NoError(t, err)
	require.True(t, laid.Changed)

	manifest := filepath.Join(laid.Dir, "harvest", "SKILL.md")

	// Something overwrote the text the session reads.
	require.NoError(t, os.WriteFile(manifest, []byte("---\nname: playbook-premium-harvest\n---\nnonsense\n"), 0o600))

	repaired, err := layer.Lay(nil, nil)
	require.NoError(t, err)
	assert.True(t, repaired.Changed, "a directory that drifted from the source has to be rebuilt")

	body, err := os.ReadFile(manifest)
	require.NoError(t, err)
	assert.Contains(t, string(body), "sell at fifteen delta")

	// And something deleted it outright.
	require.NoError(t, os.RemoveAll(laid.Dir))

	rebuilt, err := layer.Lay(nil, nil)
	require.NoError(t, err)
	assert.True(t, rebuilt.Changed)
	_, err = os.Stat(manifest)
	assert.NoError(t, err)
}

// A skill left in the directory by a wider declaration has to go, even though
// this process did not put it there - after a restart it is all that says the
// set used to be wider.
func TestASkillLeftFromAWiderSetIsNoticedOnDisk(t *testing.T) {
	source, work := t.TempDir(), t.TempDir()
	put(t, source, "harvest", "name: playbook-premium-harvest", "Sell a spread.")
	put(t, source, "envelope", "name: read-my-envelope", "Ask first.")

	target := filepath.Join(work, ".agents", "skills")
	put(t, target, "harvest", "name: playbook-premium-harvest", "Sell a spread.")
	put(t, target, "envelope", "name: read-my-envelope", "Ask first.")

	// A fresh process, so nothing is remembered: only the disk says the set is
	// wider than this declaration names.
	layer := &Layer{Source: source, AgentDir: work}
	laid, err := layer.Lay([]string{"playbook-premium-harvest"}, nil)
	require.NoError(t, err)
	assert.True(t, laid.Changed)

	_, err = os.Stat(filepath.Join(target, "envelope"))
	assert.True(t, os.IsNotExist(err))
}

// The numbers a skill is run with never reach this directory - they go into the
// task - so an edit to them must not rewrite a tree a session may be reading.
// They are still checked on every pass: dropping one is refused whether or not
// anything on disk moved.
func TestChangingANumberDoesNotRewriteTheDirectory(t *testing.T) {
	source, work := t.TempDir(), t.TempDir()
	put(t, source, "harvest", "name: playbook-premium-harvest\nrequires: [short_leg_delta]", "Sell a spread.")

	layer := &Layer{Source: source, AgentDir: work}
	first, err := layer.Lay(nil, map[string]string{"short_leg_delta": "0,15"})
	require.NoError(t, err)
	require.True(t, first.Changed)

	again, err := layer.Lay(nil, map[string]string{"short_leg_delta": "0,30"})
	require.NoError(t, err)
	assert.False(t, again.Changed, "a number changed, and no file in this directory did")

	_, err = layer.Lay(nil, nil)
	require.Error(t, err, "the number is still required on every pass")
	assert.Contains(t, err.Error(), `"short_leg_delta"`)
}
