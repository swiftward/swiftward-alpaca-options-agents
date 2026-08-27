// Package declaration reads the file that says WHEN a session runs and WHY.
//
// The declaration is the only place a wake-up time exists. Nothing here decides
// what to trade: each session carries a task in words, and the session that reads
// it decides. That line is what makes the agent autonomous rather than driven.
package declaration

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Declaration is one agent: who it is, when it wakes, which techniques it is
// given and the numbers it runs them with.
type Declaration struct {
	Kind     string    `yaml:"kind"`
	Name     string    `yaml:"name"`
	Version  string    `yaml:"version"`
	Timezone string    `yaml:"timezone"`
	Sessions []Session `yaml:"sessions"`
	// Skills are the techniques this agent is given, by the name in the skill's
	// own front matter. Empty means all the ones shipped beside it.
	//
	// They belong to the agent and not to a session on purpose, and the reason is
	// mechanical rather than a preference: the agent reads its skills directory
	// once when it starts, and one agent process serves every session, so there
	// is no directory to narrow for a single session. Which technique a session
	// uses is chosen the way it always was - the task asks for it by name.
	//
	// The set is worth narrowing because a skill costs its description in every
	// prompt of every turn, so an agent should carry its own and nobody else's.
	Skills []string `yaml:"skills"`
	// Parameters are the numbers this agent runs its skills with. A skill holds
	// the technique and an example number; the number that is actually used lives
	// here, because two accounts run the same skill on purpose and the difference
	// between them is the whole experiment.
	//
	// They stand at the top of every task, so a session cannot follow a technique
	// without seeing the numbers it belongs to.
	Parameters Parameters `yaml:"parameters"`

	location   *time.Location
	parameters string
}

// Session is one reason to wake the agent.
type Session struct {
	// Name is how this session appears in the log and the chat.
	Name string `yaml:"name"`
	// Cause is why it runs, in words the session repeats back. A session that
	// cannot say why it ran cannot be judged on whether it should have.
	Cause string `yaml:"cause"`
	// Task is what it is asked to do.
	Task string `yaml:"task"`
	// Model names the model this session is worth. Empty leaves the one the
	// conversation was opened with: a session that only reads the news does not
	// need the one that trades.
	Model string `yaml:"model"`
	// At fires once a day at this local time, "15:50".
	At string `yaml:"at"`
	// Within is how late this session may still start: an entry window survives a
	// restart at 14:35, and does not survive one at 18:00. Required with At,
	// because how late is too late differs per session and cannot be guessed here.
	Within string `yaml:"within"`
	// Days limits At to these weekdays; empty means every day the market is open.
	Days []string `yaml:"days"`
	// Every fires repeatedly, "30m", inside Between.
	Every string `yaml:"every"`
	// Between bounds Every to a window, ["09:40", "15:55"].
	Between []string `yaml:"between"`

	// parameters is the agent's own numbers, rendered once at load and put in
	// front of the task.
	parameters string

	at       time.Duration
	within   time.Duration
	every    time.Duration
	from, to time.Duration
	days     map[time.Weekday]bool
}

const kind = "trading-agent"

// Load reads and checks a declaration. Every problem is named with the session it
// belongs to: a schedule that silently drops a session is worse than one that
// refuses to load.
func Load(path string) (*Declaration, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read declaration: %w", err)
	}

	return parse(raw, path)
}

// parse is Load without the file, so the watcher can decide whether the bytes
// changed before it decides whether the schedule did.
func parse(raw []byte, path string) (*Declaration, error) {
	var d Declaration
	decoder := yaml.NewDecoder(strings.NewReader(string(raw)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&d); err != nil {
		return nil, fmt.Errorf("read declaration %s: %w", path, err)
	}

	if d.Kind != kind {
		return nil, fmt.Errorf("declaration %s is kind %q, expected %q", path, d.Kind, kind)
	}
	if d.Name == "" {
		return nil, fmt.Errorf("declaration %s has no name", path)
	}
	if len(d.Sessions) == 0 {
		return nil, fmt.Errorf("declaration %s wakes nobody: it declares no sessions", path)
	}

	if d.Timezone == "" {
		return nil, fmt.Errorf("declaration %s has no timezone: a trading hour means nothing without one", path)
	}
	location, err := time.LoadLocation(d.Timezone)
	if err != nil {
		return nil, fmt.Errorf("declaration %s names timezone %q: %w", path, d.Timezone, err)
	}
	d.location = location

	d.parameters = d.Parameters.block()
	for i := range d.Sessions {
		if err := d.Sessions[i].check(); err != nil {
			return nil, fmt.Errorf("declaration %s: %w", path, err)
		}
		d.Sessions[i].parameters = d.parameters
	}

	return &d, nil
}

// Location is the timezone every hour in this declaration is written in.
func (d *Declaration) Location() *time.Location { return d.location }

func (s *Session) check() error {
	if s.Name == "" {
		return fmt.Errorf("a session has no name")
	}
	if s.Cause == "" {
		return fmt.Errorf("session %q has no cause: it could not say why it ran", s.Name)
	}
	if s.Task == "" {
		return fmt.Errorf("session %q has no task", s.Name)
	}

	hasAt := s.At != ""
	hasEvery := s.Every != ""
	switch {
	case hasAt && hasEvery:
		return fmt.Errorf("session %q declares both at and every: one session, one way to wake", s.Name)
	case !hasAt && !hasEvery:
		return fmt.Errorf("session %q declares neither at nor every: nothing would wake it", s.Name)
	}

	var err error
	if hasAt {
		if s.at, err = parseClock(s.At); err != nil {
			return fmt.Errorf("session %q: at %q: %w", s.Name, s.At, err)
		}
		if s.Within == "" {
			return fmt.Errorf("session %q says when it starts but not how late it may still start: set within, for example 45m", s.Name)
		}
		if s.within, err = time.ParseDuration(s.Within); err != nil {
			return fmt.Errorf("session %q: within %q is not a duration: %w", s.Name, s.Within, err)
		}
		if s.within <= 0 {
			return fmt.Errorf("session %q may be started %s late, which is never", s.Name, s.within)
		}
	}
	if hasEvery {
		if s.every, err = time.ParseDuration(s.Every); err != nil {
			return fmt.Errorf("session %q: every %q is not a duration: %w", s.Name, s.Every, err)
		}
		if s.every < time.Minute {
			return fmt.Errorf("session %q wakes every %s: a session takes longer than that to think", s.Name, s.every)
		}
		if len(s.Between) != 2 {
			return fmt.Errorf("session %q wakes every %s but names no window: between needs two times", s.Name, s.every)
		}
		if s.from, err = parseClock(s.Between[0]); err != nil {
			return fmt.Errorf("session %q: between starts at %q: %w", s.Name, s.Between[0], err)
		}
		if s.to, err = parseClock(s.Between[1]); err != nil {
			return fmt.Errorf("session %q: between ends at %q: %w", s.Name, s.Between[1], err)
		}
		if s.to <= s.from {
			return fmt.Errorf("session %q has a window that ends before it starts: %s to %s", s.Name, s.Between[0], s.Between[1])
		}
	}

	s.days = map[time.Weekday]bool{}
	for _, day := range s.Days {
		weekday, ok := weekdays[strings.ToLower(strings.TrimSpace(day))]
		if !ok {
			return fmt.Errorf("session %q names day %q, which is not a weekday", s.Name, day)
		}
		s.days[weekday] = true
	}

	return nil
}

// Due reports whether this session should run at local time now, given when it
// last ran. A session that has never run carries the zero time.
func (s *Session) Due(now, last time.Time) bool {
	if len(s.days) > 0 && !s.days[now.Weekday()] {
		return false
	}

	sinceMidnight := time.Duration(now.Hour())*time.Hour +
		time.Duration(now.Minute())*time.Minute

	if s.At != "" {
		// Late is allowed, but only as late as the session says it stays useful:
		// an entry window survives a restart minutes later and means nothing hours
		// later.
		if sinceMidnight < s.at || sinceMidnight > s.at+s.within {
			return false
		}
		return last.IsZero() || last.YearDay() != now.YearDay() || last.Year() != now.Year()
	}

	if sinceMidnight < s.from || sinceMidnight > s.to {
		return false
	}

	return last.IsZero() || now.Sub(last) >= s.every
}

// Prompt is what the session is told: why it was woken, the numbers this agent
// runs on, then what to do.
//
// The numbers come before the task rather than inside it because they are the
// same in every window, and a number repeated in eight tasks is a number that
// will one day disagree with itself. A window that wants a different one says so
// in its own text, and the session is told which of the two it took.
func (s *Session) Prompt() string {
	if s.parameters == "" {
		return fmt.Sprintf("Woken by the schedule: %s\n%s", s.Cause, s.Task)
	}

	return fmt.Sprintf("Woken by the schedule: %s\n%s\n%s", s.Cause, s.parameters, s.Task)
}

// Parameters are the numbers an agent runs its skills with, read as written.
type Parameters map[string]string

// UnmarshalYAML reads the parameters and refuses everything that would make one
// ambiguous. Values may be written as numbers - a hand-edited file where 0.15
// has to be quoted is a trap - but a value that is itself a list or a mapping is
// refused: it would reach the session as Go's idea of how to print it.
func (p *Parameters) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("parameters must be a mapping of name to value")
	}

	out := Parameters{}
	for i := 0; i+1 < len(node.Content); i += 2 {
		name, value := node.Content[i], node.Content[i+1]
		if name.Kind != yaml.ScalarNode {
			return fmt.Errorf("a parameter is named by something that is not a name")
		}
		if value.Kind != yaml.ScalarNode {
			return fmt.Errorf("parameter %q holds a list or a mapping; a session is given one value", name.Value)
		}
		// A name written twice is the failure this catches: YAML keeps the last
		// one, so the number in force would be whichever line happens to be lower
		// in the file, and the other would read as if it applied.
		if _, already := out[name.Value]; already {
			return fmt.Errorf("parameter %q is named twice", name.Value)
		}
		out[name.Value] = value.Value
	}
	*p = out

	return nil
}

// block is the parameters as a session reads them. Sorted, because a prompt that
// reorders itself between turns is a prompt the agent has to read again.
func (p Parameters) block() string {
	if len(p) == 0 {
		return ""
	}

	names := make([]string, 0, len(p))
	for name := range p {
		names = append(names, name)
	}
	sort.Strings(names)

	var says strings.Builder
	says.WriteString("Numbers this agent runs on. Where a skill carries its own, this one replaces it; where your task names a different one for this window, the task wins. Say in one line which you took.\n")
	for _, name := range names {
		says.WriteString(fmt.Sprintf("- %s = %s\n", name, p[name]))
	}

	return says.String()
}

func parseClock(value string) (time.Duration, error) {
	parsed, err := time.Parse("15:04", strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("not a time of day like 09:40")
	}

	return time.Duration(parsed.Hour())*time.Hour + time.Duration(parsed.Minute())*time.Minute, nil
}

var weekdays = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
}

// Scheduled is one session as the agent itself should see it: when it will be
// woken and why. The task is not here - the session receives its task when it is
// woken, and reading a task it has not been given would invite it to act early.
type Scheduled struct {
	Name  string `json:"name" jsonschema:"how this session is named when it wakes you"`
	Cause string `json:"cause" jsonschema:"why it wakes you"`
	When  string `json:"when" jsonschema:"when it fires, in the declaration's own timezone"`
	Model string `json:"model,omitempty" jsonschema:"the model this session runs on, when it differs"`
}

// Schedule is what wakes this agent, in its own words. It exists so a session
// can answer "will I wake at the open?" by reading the declaration rather than
// by guessing - the same reason its limits are discovered rather than told.
func (d *Declaration) Schedule() []Scheduled {
	schedule := make([]Scheduled, 0, len(d.Sessions))
	for i := range d.Sessions {
		session := &d.Sessions[i]
		schedule = append(schedule, Scheduled{
			Name:  session.Name,
			Cause: session.Cause,
			When:  session.when(d.Timezone),
			Model: session.Model,
		})
	}

	return schedule
}

// when says the schedule in a sentence rather than in fields, because that is
// what the session repeats back to whoever asks.
func (s *Session) when(timezone string) string {
	var says strings.Builder
	switch {
	case s.At != "":
		says.WriteString("at " + s.At)
		if s.Within != "" {
			says.WriteString(", still valid for " + s.Within + " after that")
		}
	case s.Every != "":
		says.WriteString("every " + s.Every)
		if len(s.Between) == 2 {
			says.WriteString(" between " + s.Between[0] + " and " + s.Between[1])
		}
	}
	if len(s.Days) > 0 {
		says.WriteString(", on " + strings.Join(s.Days, ", "))
	}
	if timezone != "" {
		says.WriteString(" (" + timezone + ")")
	}

	return says.String()
}
