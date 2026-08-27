// Package skills puts the instructions a session needs only sometimes where the
// agent looks for them: `.agents/skills` inside the directory it works in.
//
// Two things live here that used to live in the shell entrypoint, and both are
// the reason it moved: which skills an agent gets is written in its declaration,
// and the entrypoint is a shell script that cannot read YAML. The second is that
// the directory may be put there from outside - see Lay - and a script that
// deletes it unconditionally deletes the operator's own files.
//
// Nothing here carries a limit. A skill describes a technique; what an agent is
// allowed to do is asked of the envelope while it works.
package skills

import (
	"bufio"
	"fmt"
	"io"
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

// Laid says what the session will read and how it got there.
type Laid struct {
	// Dir is the directory the session reads its skills from.
	Dir string
	// Names are the skills named by the declaration, in the order it named them.
	Names []string
	// Mounted is true when the directory was put there from outside this
	// process. Then nothing was written: what the session reads is whatever is
	// mounted, which is more than the declaration named and is checked as such.
	Mounted bool
}

// Lay puts the skills a declaration names where the agent reads them, inside
// agentDir.
//
// When something is mounted into that tree it is left exactly as it is. Editing
// a skill and having a working session read the new text on its next turn is the
// whole point of mounting it, and a process that rebuilt the directory would
// undo that on every restart - or, worse, delete files that belong to whoever
// mounted them. What is mounted is also what is CHECKED, and every skill in it
// is checked, not only the ones the declaration named: nothing narrowed the set,
// so the session can reach all of them and all of them need their numbers.
//
// Otherwise the directory is rebuilt from source. Rebuilt, not merged into: the
// directory it sits in outlives this image, so a skill deleted upstream has to
// disappear from the session rather than linger as an instruction nothing
// carries any more.
func Lay(source, agentDir string, wanted []string, given map[string]string) (Laid, error) {
	target := filepath.Join(agentDir, skillsUnder)
	from, mounted, err := serving(source, agentDir, target)
	if err != nil {
		return Laid{}, err
	}

	found, err := Read(from)
	if err != nil {
		return Laid{}, err
	}
	chosen, err := pick(found, wanted)
	if err != nil {
		return Laid{}, fmt.Errorf("%w (looked in %s)", err, from)
	}

	// What the session can reach is what has to hold up. With the tree mounted
	// nothing narrowed it, so a skill nobody named is reachable all the same.
	reachable := chosen
	if mounted {
		reachable = found
	}
	if err := numbersFor(reachable, given); err != nil {
		return Laid{}, err
	}

	laid := Laid{Dir: target, Mounted: mounted, Names: make([]string, 0, len(chosen))}
	for _, skill := range chosen {
		laid.Names = append(laid.Names, skill.Name)
	}
	if mounted {
		return laid, nil
	}

	// Everything above this line only reads. Nothing is removed until the whole
	// set is known to be good, so a declaration naming a skill that does not
	// exist leaves the session with the skills it had rather than with none.
	if err := os.RemoveAll(target); err != nil {
		return Laid{}, fmt.Errorf("clear %s: %w", target, err)
	}
	if len(chosen) == 0 {
		return laid, nil
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return Laid{}, fmt.Errorf("make %s: %w", target, err)
	}
	for _, skill := range chosen {
		into := filepath.Join(target, skill.Base)
		if err := os.CopyFS(into, os.DirFS(skill.Dir)); err != nil {
			return Laid{}, fmt.Errorf("copy skill %q into %s: %w", skill.Name, into, err)
		}
	}

	return laid, nil
}

// serving says which directory the session actually reads, and whether anything
// was mounted into the tree this process would otherwise rebuild.
func serving(source, agentDir, target string) (string, bool, error) {
	mounted, err := mountsInTree(agentDir, target)
	if err != nil {
		return "", false, err
	}
	if mounted {
		return target, true, nil
	}

	return source, false, nil
}

// mountsInTree is mountedInTree, as a variable so a test can prove what Lay does
// with a mounted directory without mounting one.
var mountsInTree = mountedInTree

// mountedInTree reports whether anything was mounted into the tree this process
// would otherwise rebuild: at path itself, anywhere beneath it, or at any
// directory between root and path. All three mean the same thing - the files
// there are not this process's to delete - and all three are reachable by a
// session, which is why the check is not just about path.
//
// The mount table is read rather than device numbers compared, because a bind
// mount within one filesystem keeps the device of its parent: the short trick
// would answer "not mounted" about a directory that is, and the operator's own
// files would be deleted for it.
//
// Where there is no /proc - a developer's machine, a test - nothing is mounted
// as far as this can tell, which is the answer that writes only into what this
// process itself put there.
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
			// Mounted one level down - a single skill being edited on its own.
			// Removing the tree around it fails, and would not be ours to do.
			return true, nil
		case strings.HasPrefix(path, where+string(filepath.Separator)) &&
			strings.HasPrefix(where, root+string(filepath.Separator)):
			// Mounted ABOVE the directory but below the one this process works in:
			// the tree we would rebuild lives inside somebody else's mount. The
			// work directory itself does not count - it is the volume this process
			// is given, and it is where the tree belongs.
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
