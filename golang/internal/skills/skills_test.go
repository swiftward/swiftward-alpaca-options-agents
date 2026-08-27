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

	target := filepath.Join(work, ".agents", "skills")
	laid, err := Lay(source, target,
		[]string{"playbook-premium-harvest", "read-my-envelope"},
		map[string]string{"short_leg_delta": "0,15"})
	require.NoError(t, err)

	assert.False(t, laid.Mounted)
	assert.Equal(t, []string{"playbook-premium-harvest", "read-my-envelope"}, laid.Names)

	body, err := os.ReadFile(filepath.Join(target, "harvest", "SKILL.md"))
	require.NoError(t, err)
	assert.Contains(t, string(body), "Sell a spread.")

	// The one nobody asked for costs its description in every prompt of every
	// turn, which is the whole reason the set is narrowed.
	_, err = os.Stat(filepath.Join(target, "backspread"))
	assert.True(t, os.IsNotExist(err), "a skill this agent was not given must not be there")
}

// Empty means all of them: a declaration written before this key existed keeps
// the behaviour it had.
func TestNoChoiceMeansEverySkill(t *testing.T) {
	source, work := t.TempDir(), t.TempDir()
	put(t, source, "harvest", "name: playbook-premium-harvest", "Sell a spread.")
	put(t, source, "envelope", "name: read-my-envelope", "Ask first.")

	target := filepath.Join(work, ".agents", "skills")
	laid, err := Lay(source, target, nil, nil)
	require.NoError(t, err)

	assert.Equal(t, []string{"playbook-premium-harvest", "read-my-envelope"}, laid.Names)
	for _, dir := range []string{"harvest", "envelope"} {
		_, err := os.Stat(filepath.Join(target, dir, "SKILL.md"))
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

	_, err := Lay(source, target, nil, nil)
	require.NoError(t, err)

	_, err = os.Stat(stale)
	assert.True(t, os.IsNotExist(err), "a skill this image no longer carries must be gone")
}

func TestLayRefusesASkillThatIsNotThere(t *testing.T) {
	source, work := t.TempDir(), t.TempDir()
	put(t, source, "harvest", "name: playbook-premium-harvest", "Sell a spread.")

	target := filepath.Join(work, ".agents", "skills")
	require.NoError(t, os.MkdirAll(filepath.Join(target, "harvest"), 0o755))

	_, err := Lay(source, target, []string{"playbook-defence"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `no skill called "playbook-defence"`)
	assert.Contains(t, err.Error(), `"playbook-premium-harvest"`, "the error says what is actually there")

	// Nothing was removed: the check runs before the first write, so a misspelled
	// name leaves the session with the skills it had rather than with none.
	_, statErr := os.Stat(filepath.Join(target, "harvest"))
	assert.NoError(t, statErr)
}

// A skill that quietly falls back on its own example number is how two accounts
// running the same playbook become one account run twice.
func TestLayRefusesWhenAParameterIsMissing(t *testing.T) {
	source, work := t.TempDir(), t.TempDir()
	put(t, source, "harvest",
		"name: playbook-premium-harvest\nrequires: [short_leg_delta, min_edge_points]", "Sell a spread.")

	_, err := Lay(source, filepath.Join(work, ".agents", "skills"), nil,
		map[string]string{"short_leg_delta": "0,15"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"playbook-premium-harvest"`)
	assert.Contains(t, err.Error(), `"min_edge_points"`)
}

func TestCheckRefusesASkillNamedTwice(t *testing.T) {
	source := t.TempDir()
	put(t, source, "harvest", "name: playbook-premium-harvest", "Sell a spread.")

	_, err := Check(source, []string{"playbook-premium-harvest", "playbook-premium-harvest"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "named twice")
}

// The point of mounting the directory is that an edit to a skill reaches a
// session that is already running. A process that rebuilt it would undo that on
// every restart, and would delete files belonging to whoever mounted them.
func TestAMountedDirectoryIsLeftExactlyAsItIs(t *testing.T) {
	source, work := t.TempDir(), t.TempDir()
	put(t, source, "harvest", "name: playbook-premium-harvest", "the version inside the image")

	target := filepath.Join(work, ".agents", "skills")
	put(t, target, "harvest", "name: playbook-premium-harvest", "the version being edited")
	put(t, target, "draft", "name: playbook-draft", "not in the image at all")

	mountedAt = func(path string) (bool, error) { return path == target, nil }
	t.Cleanup(func() { mountedAt = Mounted })

	laid, err := Lay(source, target, []string{"playbook-premium-harvest"}, nil)
	require.NoError(t, err)
	assert.True(t, laid.Mounted)

	body, err := os.ReadFile(filepath.Join(target, "harvest", "SKILL.md"))
	require.NoError(t, err)
	assert.Contains(t, string(body), "the version being edited", "the mounted text must not be replaced by the image's")

	_, err = os.Stat(filepath.Join(target, "draft"))
	assert.NoError(t, err, "nothing in a mounted directory is ours to delete")
}

// What is checked is what the session reads. With a directory mounted over it,
// the image's own copy is not it.
func TestAMountedDirectoryIsTheOneChecked(t *testing.T) {
	source, work := t.TempDir(), t.TempDir()
	put(t, source, "harvest", "name: playbook-premium-harvest", "in the image")

	target := filepath.Join(work, ".agents", "skills")
	put(t, target, "harvest", "name: playbook-premium-harvest\nrequires: [short_leg_delta]", "being edited")

	mountedAt = func(path string) (bool, error) { return path == target, nil }
	t.Cleanup(func() { mountedAt = Mounted })

	_, err := Lay(source, target, []string{"playbook-premium-harvest"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"short_leg_delta"`,
		"the requirement added in the mounted copy is the one that counts")
}

// A bind mount within one filesystem keeps the device of its parent, so the
// device comparison would answer "not mounted" about a directory that is - and
// the operator's own files would be deleted for it. The mount table names mount
// points exactly, which is why it is read instead.
func TestTheMountTableIsReadForExactMountPoints(t *testing.T) {
	table := strings.Join([]string{
		"24 30 0:22 / /proc rw,nosuid,nodev,noexec,relatime - proc proc rw",
		"31 30 0:26 / /work rw,relatime - ext4 /dev/sdb rw",
		"32 31 0:27 /agent/skills /work/.agents/skills ro,relatime - ext4 /dev/sda rw",
		`33 30 0:28 / /mnt/two\040words rw,relatime - ext4 /dev/sdc rw`,
	}, "\n")

	for path, want := range map[string]bool{
		"/work/.agents/skills":  true,
		"/work/.agents/skills/": true,
		"/work":                 true,
		"/work/.agents":         false,
		"/work/.agents/skills/playbook-premium-harvest": false,
		"/mnt/two words": true,
	} {
		got, err := mountedIn(strings.NewReader(table), path)
		require.NoError(t, err)
		assert.Equal(t, want, got, path)
	}
}

// Where there is no mount table - a developer's machine, this test - nothing is
// mounted as far as this can tell, which is the answer that writes only into
// what this process itself put there.
func TestNoMountTableMeansNothingIsMounted(t *testing.T) {
	mounted, err := Mounted(t.TempDir())
	require.NoError(t, err)
	assert.False(t, mounted)
}
