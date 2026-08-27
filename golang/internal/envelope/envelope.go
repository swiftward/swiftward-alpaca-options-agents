// Package envelope answers one question for the trading session: what am I
// allowed to do right now, and by which version of the rules.
//
// The answer belongs to the policy gateway, which is not yet in front of the
// broker. Until it is, this package stands in its place: it serves the same tool
// under the same server name, so the session cannot tell the difference and does
// not have to be rewritten when the gateway arrives.
//
// What it does NOT do is as important as what it does. It does not judge orders,
// it does not refuse anything and it does not rewrite what the broker advertises.
// Those belong to the gateway. This serves the envelope and nothing else.
package envelope

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Envelope is what one caller may do on one tool, right now.
type Envelope struct {
	Tool string `json:"tool" jsonschema:"the tool these limits apply to"`
	// Identity is the caller the gateway resolved. Two accounts run the same
	// engine under different limits, so an envelope that does not say whose it is
	// cannot be compared with what was actually traded.
	Identity string `json:"identity" jsonschema:"the caller these limits were computed for"`
	// RulesetVersion is which set of rules produced these numbers. A refusal names
	// the same version, which is the only way to tell "the rules changed" from
	// "the agent misread them".
	RulesetVersion string `json:"ruleset_version" jsonschema:"the version of the ruleset that produced these limits"`
	// Governed says whether this tool is governed at all. False with no
	// constraints means nobody is watching; false is never a reason to assume
	// freedom, because an envelope that could not be computed says the same.
	Governed    bool         `json:"governed" jsonschema:"whether this tool is governed by a ruleset at all"`
	Constraints []Constraint `json:"constraints" jsonschema:"every limit disclosed to you; a limit disclosed to nobody does not appear here"`
}

// Constraint is one limit, disclosed as far as its rule allows.
//
// Disclosure is per rule and decided by the rule's author, not by the caller:
// `boundary` shows the number, `existence` says only that the rule is there, and
// a rule disclosed to nobody is absent from the list entirely. So an empty list
// means "nothing was disclosed to you" and never "there is nothing".
type Constraint struct {
	Rule       string `json:"rule" yaml:"rule" jsonschema:"the rule's name"`
	Disclosure string `json:"disclosure" yaml:"disclosure" jsonschema:"boundary if you may see the number, existence if you may only know the rule is there"`
	Subject    string `json:"subject,omitempty" yaml:"subject" jsonschema:"the quantity this limits"`
	Kind       string `json:"kind,omitempty" yaml:"kind" jsonschema:"maximum, minimum, enum or range"`
	Value      any    `json:"value,omitempty" yaml:"value" jsonschema:"the limit itself: a number, a list, or an object with min and max"`
	Unit       string `json:"unit,omitempty" yaml:"unit" jsonschema:"what the number is measured in"`
	Says       string `json:"says,omitempty" yaml:"says" jsonschema:"what an existence rule is allowed to tell you"`
}

// Disclosure values. A rule declared none never reaches the caller, so it has no
// name here: absence is how it appears.
const (
	Boundary  = "boundary"
	Existence = "existence"
)

// The closed vocabularies. Closed on purpose: a subject or a unit nobody agreed
// on is a rule that means one thing to the engine and another to the session,
// and the session cannot see the difference. A value outside these lists fails to
// load rather than reaching an agent.
var (
	subjects = []string{"position_max_loss", "portfolio_max_loss", "underlying", "expiration"}
	units    = []string{"percent_of_equity", "trading_days_from_today"}
	kinds    = []string{"maximum", "minimum", "enum", "range"}
)

// Ruleset is the file an operator edits: one version, and what each caller may do.
type Ruleset struct {
	Version string           `yaml:"ruleset_version"`
	Agents  map[string]Agent `yaml:"agents"`
}

// Agent is one caller's limits, per tool.
type Agent struct {
	Tools map[string][]Constraint `yaml:"tools"`
}

// Load reads the ruleset and checks it against the contract. It is read on every
// call rather than at startup: lowering a ceiling is one edit to one file, and it
// takes effect without restarting anything - which is the whole point of the
// limits living outside the agent.
func Load(path string) (Ruleset, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Ruleset{}, fmt.Errorf("read ruleset: %w", err)
	}

	var set Ruleset
	if err := yaml.Unmarshal(raw, &set); err != nil {
		return Ruleset{}, fmt.Errorf("parse ruleset %s: %w", path, err)
	}
	if strings.TrimSpace(set.Version) == "" {
		return Ruleset{}, fmt.Errorf("%s: ruleset_version is required: an envelope without its version cannot be compared with a refusal", path)
	}
	if len(set.Agents) == 0 {
		return Ruleset{}, fmt.Errorf("%s: no agents declared: a ruleset nobody is under governs nothing", path)
	}

	for identity, agent := range set.Agents {
		for tool, constraints := range agent.Tools {
			seen := map[string]bool{}
			for _, c := range constraints {
				if err := c.check(); err != nil {
					return Ruleset{}, fmt.Errorf("%s: %s on %s: %w", path, identity, tool, err)
				}
				if seen[c.Rule] {
					return Ruleset{}, fmt.Errorf("%s: %s on %s: rule %q declared twice", path, identity, tool, c.Rule)
				}
				seen[c.Rule] = true
			}
		}
	}
	return set, nil
}

func (c Constraint) check() error {
	if strings.TrimSpace(c.Rule) == "" {
		return fmt.Errorf("a constraint without a rule name cannot be named by a refusal")
	}
	switch c.Disclosure {
	case Existence:
		// Existence discloses that a rule is there and not one parameter of it.
		// Carrying a subject or a number here would disclose a boundary while
		// claiming not to, which is worse than either honest answer.
		if c.Subject != "" || c.Kind != "" || c.Value != nil || c.Unit != "" {
			return fmt.Errorf("rule %q is disclosed as existence and may carry no subject, kind, value or unit", c.Rule)
		}
	case Boundary:
		if c.Value == nil {
			return fmt.Errorf("rule %q is disclosed as boundary and must carry a value", c.Rule)
		}
		if !known(subjects, c.Subject) {
			return fmt.Errorf("rule %q: subject %q is not one of %s", c.Rule, c.Subject, strings.Join(subjects, ", "))
		}
		if !known(kinds, c.Kind) {
			return fmt.Errorf("rule %q: kind %q is not one of %s", c.Rule, c.Kind, strings.Join(kinds, ", "))
		}
		if c.Unit != "" && !known(units, c.Unit) {
			return fmt.Errorf("rule %q: unit %q is not one of %s", c.Rule, c.Unit, strings.Join(units, ", "))
		}
		if c.Says != "" {
			return fmt.Errorf("rule %q shows its boundary and does not need a sentence about itself", c.Rule)
		}
	default:
		return fmt.Errorf("rule %q: disclosure %q is neither boundary nor existence; a rule disclosed to nobody is left out of the file", c.Rule, c.Disclosure)
	}
	return nil
}

func known(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// bareTool drops the namespace a client puts in front of a tool's name.
//
// A limit belongs to the tool the BROKER publishes, not to the client that
// happens to be calling it: the same order placed through a client that
// namespaces its tools is the same order. Measured 27 August - a session that
// restarted its conversation began asking about
// `mcp__broker__place_option_order`, was told the tool was under no ruleset, and
// stopped trading for an hour. "Nothing was disclosed to you" reads exactly like
// permission, which is why this must match rather than fall through.
func bareTool(tool string) string {
	if at := strings.LastIndex(tool, "__"); at >= 0 {
		return tool[at+len("__"):]
	}

	return tool
}

// For computes one caller's envelope on one tool.
//
// An identity that does not resolve is an error and never an empty envelope: a
// caller nobody recognises must not be told "no limits were disclosed", because
// that reads exactly like a tool that is simply not governed.
func (r Ruleset) For(identity, tool string) (Envelope, error) {
	agent, ok := r.Agents[identity]
	if !ok {
		return Envelope{}, fmt.Errorf("no envelope for %q: this caller is under no ruleset", identity)
	}

	constraints, governed := agent.Tools[bareTool(tool)]

	// Same inputs, same bytes: the answer is sorted so a session comparing two
	// reads sees a changed rule, not a changed order. It is built empty rather
	// than nil so that "nothing was disclosed to you" crosses as a list with
	// nothing in it, never as a missing field.
	sorted := make([]Constraint, 0, len(constraints))
	sorted = append(sorted, constraints...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Rule < sorted[j].Rule })

	return Envelope{
		Tool:           tool,
		Identity:       identity,
		RulesetVersion: r.Version,
		Governed:       governed,
		Constraints:    sorted,
	}, nil
}
