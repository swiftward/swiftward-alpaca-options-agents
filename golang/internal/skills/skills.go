// Package skills puts the instructions a session needs only sometimes where the
// agent looks for them: `.agents/skills` inside the directory it works in.
//
// Two things live here that used to live in the shell entrypoint, and both are
// the reason it moved: which skills an agent gets is written in its declaration,
// and the entrypoint is a shell script that cannot read YAML. The second is that
// the directory may be put there from outside - see Mounted - and a script that
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

// manifest is the file the agent reads. The name is the agent's, not ours.
const manifest = "SKILL.md"

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

	chosen := found
	if len(wanted) > 0 {
		byName := map[string]Skill{}
		for _, skill := range found {
			byName[skill.Name] = skill
		}
		chosen = make([]Skill, 0, len(wanted))
		seen := map[string]bool{}
		for _, name := range wanted {
			if seen[name] {
				return nil, fmt.Errorf("skill %q is named twice", name)
			}
			seen[name] = true
			skill, ok := byName[name]
			if !ok {
				return nil, fmt.Errorf("no skill called %q in %s; there is %s",
					name, source, listOf(found))
			}
			chosen = append(chosen, skill)
		}
	}

	// The numbers a technique is run with belong to the agent, not to the file
	// that describes the technique. A skill that asks for one it was not given
	// would fall back on its own example number, and the two accounts this
	// project runs side by side exist precisely so those numbers can differ.
	for _, skill := range chosen {
		for _, needed := range skill.Requires {
			if _, ok := given[needed]; !ok {
				return nil, fmt.Errorf("skill %q needs the parameter %q and the declaration does not give it", skill.Name, needed)
			}
		}
	}

	return chosen, nil
}

// Laid says what the session will read and how it got there.
type Laid struct {
	// Names are the skills the session can now reach, in the order chosen.
	Names []string
	// Mounted is true when the directory was put there from outside this
	// process. Then nothing was written: what the session reads is whatever is
	// mounted, and the check above was made against that rather than against
	// what this image carries.
	Mounted bool
}

// Lay puts the chosen skills where the agent reads them.
//
// When target is a mount point it is left exactly as it is. Editing a skill and
// having a working session read the new text on its next turn is the whole point
// of mounting it, and a process that replaced the directory would undo that on
// every restart - or, worse, delete files that belong to whoever mounted them.
//
// Otherwise the directory is rebuilt from source. Rebuilt, not merged into: the
// directory it sits in outlives this image, so a skill deleted upstream has to
// disappear from the session rather than linger as an instruction nothing
// carries any more.
func Lay(source, target string, wanted []string, given map[string]string) (Laid, error) {
	from, mounted, err := Serving(source, target)
	if err != nil {
		return Laid{}, err
	}

	chosen, err := Check(from, wanted, given)
	if err != nil {
		return Laid{}, err
	}

	laid := Laid{Mounted: mounted, Names: make([]string, 0, len(chosen))}
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

// Serving says which directory the session actually reads, and whether it was
// put there from outside. With something mounted over the target, this image's
// own copy is not what the session reads and must not be what is checked: a
// skill added to the mounted checkout is there, and a skill the image carries
// but the mount does not is not.
func Serving(source, target string) (string, bool, error) {
	mounted, err := mountedAt(target)
	if err != nil {
		return "", false, err
	}
	if mounted {
		return target, true, nil
	}

	return source, false, nil
}

// mountedAt is Mounted, as a variable so a test can prove what Lay does with a
// mounted directory without mounting one.
var mountedAt = Mounted

// Mounted reports whether path is a mount point - whether a filesystem was put
// there from outside this process.
//
// It is read from /proc/self/mountinfo, which names mount points exactly.
// Comparing the device of the directory against its parent's is the shorter
// trick and the wrong one: a bind mount within one filesystem keeps the device,
// so the check would answer "not mounted" about a directory that is, and the
// operator's files would be deleted for it.
//
// Where there is no /proc - a developer's machine, a test - nothing is mounted
// as far as this can tell, which is the answer that writes only into what this
// process itself put there.
func Mounted(path string) (bool, error) {
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read the mount table: %w", err)
	}
	defer func() { _ = file.Close() }()

	return mountedIn(file, path)
}

// mountedIn answers Mounted's question about an already-open mount table. Each
// line of mountinfo carries the mount point as its fifth field, with space, tab,
// newline and backslash written as octal escapes.
func mountedIn(table io.Reader, path string) (bool, error) {
	want := filepath.Clean(path)

	scanner := bufio.NewScanner(table)
	// Mount points can be long, and a line that does not fit would be read as two
	// lines, neither of which is a mount point.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 5 {
			continue
		}
		if unescape(fields[4]) == want {
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
