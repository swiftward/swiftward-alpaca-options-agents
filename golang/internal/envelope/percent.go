package envelope

import (
	"fmt"
)

// PercentOfEquity is what a named rule allows, as a share of what the account is
// worth. It is how a limit stated in the ruleset becomes a number in dollars.
//
// The unit is checked rather than assumed: the same rule could one day be stated
// in dollars, and reading a dollar figure as a percentage would raise a ceiling
// by a factor of a thousand without a word.
func (e Envelope) PercentOfEquity(rule string) (float64, error) {
	for _, constraint := range e.Constraints {
		if constraint.Rule != rule {
			continue
		}
		if constraint.Unit != PercentOfEquity {
			return 0, fmt.Errorf("%s is stated in %q, not in %s", rule, constraint.Unit, PercentOfEquity)
		}
		share, ok := constraint.Value.(float64)
		if !ok {
			// YAML gives a whole number as an int, and both are percentages.
			whole, isInt := constraint.Value.(int)
			if !isInt {
				return 0, fmt.Errorf("%s does not carry a number: %v", rule, constraint.Value)
			}
			share = float64(whole)
		}
		if share <= 0 {
			return 0, fmt.Errorf("%s allows nothing: %v", rule, constraint.Value)
		}

		return share, nil
	}

	return 0, fmt.Errorf("this envelope carries no rule called %s", rule)
}

// PercentOfEquity is the unit a share of the account is stated in.
const PercentOfEquity = "percent_of_equity"
