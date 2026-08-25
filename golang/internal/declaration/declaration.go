// Package declaration reads the file that says WHEN a session runs and WHY.
//
// The declaration is the only place a wake-up time exists. Nothing here decides
// what to trade: each session carries a task in words, and the session that reads
// it decides. That line is what makes the agent autonomous rather than driven.
package declaration

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Declaration is one agent: who it is and when it wakes.
type Declaration struct {
	Kind     string    `yaml:"kind"`
	Name     string    `yaml:"name"`
	Version  string    `yaml:"version"`
	Timezone string    `yaml:"timezone"`
	Sessions []Session `yaml:"sessions"`

	location *time.Location
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

	for i := range d.Sessions {
		if err := d.Sessions[i].check(); err != nil {
			return nil, fmt.Errorf("declaration %s: %w", path, err)
		}
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

// Prompt is what the session is told: why it was woken, then what to do.
func (s *Session) Prompt() string {
	return fmt.Sprintf("Woken by the schedule: %s\n%s", s.Cause, s.Task)
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
