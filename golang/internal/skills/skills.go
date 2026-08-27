// Package skills puts the instructions a session needs only sometimes where the
// agent looks for them: `.agents/skills` inside the directory it works in.
//
// Two things live here that used to live in the shell entrypoint, and both are
// the reason it moved: which skills an agent gets is written in its declaration,
// and the entrypoint is a shell script that cannot read YAML. The second is that
// the text of a skill is edited while a session runs, and keeping the directory
// level with it is a job for something that can tell an edit from a restart.
//
// Nothing here carries a limit. A skill describes a technique; what an agent is
// allowed to do is asked of the envelope while it works.
package skills

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// manifest is the file the agent reads, and skillsUnder is where it looks for
// them inside the directory it works in. Both names are the agent's, not ours.
const (
	manifest    = "SKILL.md"
	skillsUnder = ".agents/skills"
	// building is where the next set is assembled, and retiring is where the one
	// in use waits while the swap happens. Both sit beside the target so each
	// move is a rename inside one directory rather than a copy across one.
	building = ".agents/skills.next"
	retiring = ".agents/skills.old"
)

// Skill is one directory of instructions, as its own SKILL.md describes it.
type Skill struct {
	// Name is what the declaration names it by. It comes from the front matter
	// and not from the directory: the front matter is what the agent itself
	// reads, and naming a skill by anything else would let the two disagree.
	Name string
	// Requires are the parameters a task must carry for this skill to be
	// followed. The skill holds the technique; the numbers belong to the agent
	// running it, and a skill that silently falls back on its own is how two
	// accounts quietly become one.
	Requires []string
	// Dir is where the skill lives, and Base is that directory's own name.
	Dir, Base string
}

// header is the front matter of a SKILL.md. Unknown fields are allowed on
// purpose: the format belongs to the agent that reads it, and a field it grows
// must not stop this project from starting.
type header struct {
	Name     string   `yaml:"name"`
	Requires []string `yaml:"requires"`
}

// Read reads every skill under source. A directory without a SKILL.md is not a
// skill and is passed over; a SKILL.md that cannot be read is an error, because
// the alternative is a session quietly missing an instruction.
func Read(source string) ([]Skill, error) {
	entries, err := os.ReadDir(source)
	if err != nil {
		if os.IsNotExist(err) {
			// Carrying no skills at all is a state this project is allowed to be
			// in: git keeps no empty directory, so deleting the last skill deletes
			// the tree, and a session with no skills is a working session.
			return nil, nil
		}
		return nil, fmt.Errorf("read the skills in %s: %w", source, err)
	}

	var found []Skill
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(source, entry.Name())
		path := filepath.Join(dir, manifest)
		raw, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		head, err := frontMatter(raw)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		if head.Name == "" {
			return nil, fmt.Errorf("%s names no skill: the declaration has nothing to ask for it by", path)
		}
		found = append(found, Skill{
			Name: head.Name, Requires: head.Requires, Dir: dir, Base: entry.Name(),
		})
	}

	sort.Slice(found, func(i, j int) bool { return found[i].Name < found[j].Name })
	for i := 1; i < len(found); i++ {
		if found[i].Name == found[i-1].Name {
			return nil, fmt.Errorf("two skills call themselves %q: %s and %s",
				found[i].Name, found[i-1].Dir, found[i].Dir)
		}
	}

	return found, nil
}

// frontMatter reads the YAML block a SKILL.md opens with, between two lines of
// three dashes.
func frontMatter(raw []byte) (header, error) {
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return header{}, fmt.Errorf("no front matter: the file must open with a line of three dashes")
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return header{}, fmt.Errorf("the front matter is never closed by a line of three dashes")
	}

	var head header
	if err := yaml.Unmarshal([]byte(rest[:end]), &head); err != nil {
		return header{}, fmt.Errorf("the front matter is not readable: %w", err)
	}

	return head, nil
}

// Check picks the skills wanted out of source and refuses anything that would
// leave a session reading half an instruction: a name nothing answers to, or a
// skill whose parameters the declaration does not give.
//
// An empty wanted means all of them - a declaration written before this key
// existed keeps the behaviour it had.
func Check(source string, wanted []string, given map[string]string) ([]Skill, error) {
	found, err := Read(source)
	if err != nil {
		return nil, err
	}
	chosen, err := pick(found, wanted)
	if err != nil {
		return nil, err
	}
	if err := numbersFor(chosen, given); err != nil {
		return nil, err
	}

	return chosen, nil
}

// pick takes the skills named out of the ones found, and refuses a name nothing
// answers to by saying what is actually there - a misspelling is then one line
// to fix rather than a hunt through the image.
func pick(found []Skill, wanted []string) ([]Skill, error) {
	if len(wanted) == 0 {
		return found, nil
	}

	byName := map[string]Skill{}
	for _, skill := range found {
		byName[skill.Name] = skill
	}

	chosen := make([]Skill, 0, len(wanted))
	seen := map[string]bool{}
	for _, name := range wanted {
		if seen[name] {
			return nil, fmt.Errorf("skill %q is named twice", name)
		}
		seen[name] = true
		skill, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("no skill called %q; there is %s", name, listOf(found))
		}
		chosen = append(chosen, skill)
	}

	return chosen, nil
}

// numbersFor refuses a skill whose parameters the declaration does not give.
// The numbers a technique is run with belong to the agent, not to the file that
// describes the technique: a skill that asks for one it was not given would fall
// back on its own example number, and the two accounts this project runs side by
// side exist precisely so those numbers can differ.
func numbersFor(reachable []Skill, given map[string]string) error {
	for _, skill := range reachable {
		for _, needed := range skill.Requires {
			if _, ok := given[needed]; !ok {
				return fmt.Errorf("skill %q needs the parameter %q and the declaration does not give it", skill.Name, needed)
			}
		}
	}

	return nil
}

// Laid says what the session can read after a pass, and whether that pass had
// anything to do.
type Laid struct {
	// Dir is the directory the session reads its skills from.
	Dir string
	// Names are the skills it can now reach, in the order the declaration named
	// them.
	Names []string
	// Changed is false when the source, the choice and the numbers were all the
	// same as last time and nothing was written. Most passes are like that: the
	// clock asks once a minute and a skill is edited a few times a week.
	Changed bool
}

// Layer keeps the directory the agent reads equal to the skills a declaration
// names, out of the source this deployment was given.
//
// It is asked on every tick of the harness clock rather than once at start, and
// that is the whole point of it. A session opens SKILL.md while it works, so an
// edit to the text of a technique can reach a session that is already running -
// no rebuild of the image, no restart of the process. What it must NOT do is
// rewrite the directory once a minute for nothing, so it compares before it
// writes.
//
// The comparison is against what is ON DISK, not against what this process
// remembers laying. The session works in that directory and can write there, so
// a remembered answer would let a damaged copy stand for the rest of the day
// while every tick reported nothing to do.
//
// The source is a directory this process only reads. Mounting a checkout there
// is how an edit gets in; mounting one over the target instead is refused, see
// Lay - the choice a declaration makes has to mean the same thing on a developer
// machine and on a deployment, and a directory put there from outside is one
// this process cannot narrow.
type Layer struct {
	// Source is where the skills come from, and AgentDir is the directory the
	// session works in.
	Source, AgentDir string
}

// Lay makes the directory the agent reads hold exactly the skills wanted, with
// the text the source holds now.
//
// Nothing is removed until the whole set is known to be good: a declaration
// naming a skill that does not exist, or failing to give a number one requires,
// leaves the session with the skills it had rather than with none.
func (l *Layer) Lay(wanted []string, given map[string]string) (Laid, error) {
	target := filepath.Join(l.AgentDir, skillsUnder)

	// Mounting over the target is the one arrangement this refuses, and it is
	// refused rather than handled. Handled, it would mean the session reads
	// whatever is mounted - every skill in the checkout, not the ones the
	// declaration named - so the same declaration would behave one way on a
	// developer machine and another on a deployment. That is the class of
	// difference that is found at the worst possible moment.
	if err := refuseAMountedTarget(l.AgentDir, target); err != nil {
		return Laid{}, err
	}

	found, err := Read(l.Source)
	if err != nil {
		return Laid{}, err
	}
	chosen, err := pick(found, wanted)
	if err != nil {
		return Laid{}, fmt.Errorf("%w (looked in %s)", err, l.Source)
	}
	// Checked on every pass, before anything is compared: a number is dropped
	// from a declaration without a single file moving, and that has to be caught
	// the moment it happens rather than the next time a skill is edited.
	if err := numbersFor(chosen, given); err != nil {
		return Laid{}, err
	}

	names := make([]string, 0, len(chosen))
	from := make([]tree, 0, len(chosen))
	for _, skill := range chosen {
		names = append(names, skill.Name)
		from = append(from, tree{base: skill.Base, dir: skill.Dir})
	}

	want, err := stampOf(from)
	if err != nil {
		return Laid{}, err
	}
	// A target that cannot be read is a target that has to be rebuilt, which is
	// what an unreadable one gets by not matching.
	have, _ := stampOfLaid(target)
	if want == have {
		return Laid{Dir: target, Names: names, Changed: false}, nil
	}

	if err := writeInto(l.AgentDir, target, chosen); err != nil {
		return Laid{}, err
	}

	return Laid{Dir: target, Names: names, Changed: true}, nil
}

// writeInto builds the set beside the one in use and swaps it in by renaming,
// keeping the old one until the swap is done.
//
// A session reads these files while it works. Deleting the directory it is
// reading and then copying a new one into place would leave it missing for as
// long as the copy takes, and would leave it missing for good if the copy failed
// halfway. Two renames inside one directory cost the same either way, and
// between them the old set is still on disk to be put back.
func writeInto(agentDir, target string, chosen []Skill) error {
	next := filepath.Join(agentDir, building)
	previous := filepath.Join(agentDir, retiring)
	if err := os.RemoveAll(next); err != nil {
		return fmt.Errorf("clear %s: %w", next, err)
	}
	if err := os.MkdirAll(next, 0o755); err != nil {
		return fmt.Errorf("make %s: %w", next, err)
	}
	// Whatever happens below, no half-built set is left on disk pretending to be
	// a skill nobody chose.
	defer func() { _ = os.RemoveAll(next) }()

	for _, skill := range chosen {
		into := filepath.Join(next, skill.Base)
		if err := os.CopyFS(into, os.DirFS(skill.Dir)); err != nil {
			return fmt.Errorf("copy skill %q into %s: %w", skill.Name, into, err)
		}
	}

	if err := os.RemoveAll(previous); err != nil {
		return fmt.Errorf("clear %s: %w", previous, err)
	}
	// Replaced whole, not merged into: the directory outlives the image, so a
	// skill deleted upstream has to disappear from the session rather than linger
	// as an instruction nothing carries any more.
	swapped := false
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, previous); err != nil {
			return fmt.Errorf("move %s aside: %w", target, err)
		}
		swapped = true
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("look at %s: %w", target, err)
	}

	if err := os.Rename(next, target); err != nil {
		// The session keeps the skills it had. Losing them because a rename failed
		// would be the worst of both: no instructions, and no sign of why.
		if swapped {
			_ = os.Rename(previous, target)
		}
		return fmt.Errorf("put %s in place: %w", target, err)
	}
	if err := os.RemoveAll(previous); err != nil {
		return fmt.Errorf("clear %s: %w", previous, err)
	}

	return nil
}

// tree is one skill's directory as it will sit under the target: the name it
// gets there, and where its files are read from now.
type tree struct{ base, dir string }

// stampOf fingerprints exactly what would end up on disk: which skills were
// chosen and the text of every file in each, keyed by the path each file will
// have under the target. Nothing else belongs here - the numbers a skill is run
// with are checked separately and never reach this directory, so hashing them
// would rebuild a tree a session may be reading over an edit that could not have
// changed it.
//
// The text is hashed rather than dated because a modification time answers a
// different question: `touch` would rebuild the directory, and a file restored
// with an old date would not.
func stampOf(trees []tree) (string, error) {
	var files []named
	for _, one := range trees {
		from := os.DirFS(one.dir)
		err := fs.WalkDir(from, ".", func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			body, err := fs.ReadFile(from, path)
			if err != nil {
				return err
			}
			files = append(files, named{path: one.base + "/" + path, body: body})

			return nil
		})
		if err != nil {
			return "", fmt.Errorf("read the skills in %s: %w", one.dir, err)
		}
	}

	return hashOf(files), nil
}

// stampOfLaid fingerprints the directory the session actually reads, in the same
// terms, so the two can be compared. A directory that is not there yet is not an
// error - it is a directory that does not match, which is the answer that builds
// it.
func stampOfLaid(target string) (string, error) {
	var files []named
	from := os.DirFS(target)
	err := fs.WalkDir(from, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || path == "." {
			return nil
		}
		body, err := fs.ReadFile(from, path)
		if err != nil {
			return err
		}
		files = append(files, named{path: path, body: body})

		return nil
	})
	if err != nil {
		return "", err
	}

	return hashOf(files), nil
}

type named struct {
	path string
	body []byte
}

// hashOf is the one place the two fingerprints are computed, so they cannot
// drift into answering different questions. Sorted by path: the order files are
// walked in is not the order they were chosen in.
func hashOf(files []named) string {
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })

	sum := sha256.New()
	for _, file := range files {
		fmt.Fprintf(sum, "%s\x00%d\x00", file.path, len(file.body))
		sum.Write(file.body)
	}

	return hex.EncodeToString(sum.Sum(nil))
}

// refuseAMountedTarget stops the process rather than deleting files that are not
// its own, and says where to mount instead.
//
// The mount table is read rather than device numbers compared, because a bind
// mount within one filesystem keeps the device of its parent: the short trick
// would answer "not mounted" about a directory that is. Three shapes count and
// they count for the same reason - at the directory, anywhere beneath it, or
// between it and the directory the session works in. The last one is the one
// that would cost the most: the tree this rebuilds would be sitting inside
// somebody else's mount.
//
// Where there is no /proc - a developer's machine, a test - nothing is mounted
// as far as this can tell, which is the answer that writes only into what this
// process itself put there.
func refuseAMountedTarget(agentDir, target string) error {
	mounted, err := mountsInTree(agentDir, target)
	if err != nil {
		return err
	}
	if mounted {
		return fmt.Errorf("something is mounted at or around %s, and this process rebuilds that directory: "+
			"mount the checkout where the skills are READ from instead (SKILLS_DIR), so the declaration's choice of skills still applies", target)
	}

	return nil
}

// mountsInTree is mountedInTree, as a variable so a test can prove what Lay does
// about a mounted directory without mounting one.
var mountsInTree = mountedInTree

func mountedInTree(root, path string) (bool, error) {
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read the mount table: %w", err)
	}
	defer func() { _ = file.Close() }()

	return mountedInTreeFrom(file, root, path)
}

// mountedInTreeFrom answers the question above about an already-open mount
// table. Each line of mountinfo carries the mount point as its fifth field, with
// space, tab, newline and backslash written as octal escapes.
func mountedInTreeFrom(table io.Reader, root, path string) (bool, error) {
	root, path = filepath.Clean(root), filepath.Clean(path)

	scanner := bufio.NewScanner(table)
	// Mount points can be long, and a line that does not fit would be read as two
	// lines, neither of which is a mount point.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 5 {
			continue
		}
		switch where := unescape(fields[4]); {
		case where == path:
			return true, nil
		case strings.HasPrefix(where, path+string(filepath.Separator)):
			return true, nil
		case strings.HasPrefix(path, where+string(filepath.Separator)) &&
			strings.HasPrefix(where, root+string(filepath.Separator)):
			// The work directory itself does not count - it is the volume this
			// process is given, and it is where the tree belongs.
			return true, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("read the mount table: %w", err)
	}

	return false, nil
}

// unescape turns the octal escapes mountinfo writes back into the characters
// they stand for. A path with a space in it is otherwise two fields.
func unescape(field string) string {
	if !strings.ContainsRune(field, '\\') {
		return field
	}

	var out strings.Builder
	for i := 0; i < len(field); {
		if field[i] == '\\' && i+3 < len(field) {
			if code, err := strconv.ParseUint(field[i+1:i+4], 8, 8); err == nil {
				out.WriteByte(byte(code))
				i += 4
				continue
			}
		}
		out.WriteByte(field[i])
		i++
	}

	return out.String()
}

// listOf names what is actually there, so a misspelled skill is one line to fix
// rather than a hunt through the image.
func listOf(found []Skill) string {
	if len(found) == 0 {
		return "no skill at all"
	}
	names := make([]string, 0, len(found))
	for _, skill := range found {
		names = append(names, strconv.Quote(skill.Name))
	}

	return strings.Join(names, ", ")
}
