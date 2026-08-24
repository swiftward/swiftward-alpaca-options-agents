package agent

// ThreadOptions are the bounds a conversation runs under. They are set when the
// thread opens and again when it is resumed: a continued conversation that lost
// its sandbox would be less bounded than the one that started it.
type ThreadOptions struct {
	Model   string
	Sandbox string
	Dir     string
	// ApprovalPolicy decides what happens when the agent wants to act outside its
	// sandbox. Nobody sits at this keyboard, so a policy that ASKS stalls the turn
	// forever - the bound is the sandbox, and the answer to a request to leave it
	// is no.
	ApprovalPolicy string
}

func (o ThreadOptions) params() map[string]any {
	params := map[string]any{}
	if o.Model != "" {
		params["model"] = o.Model
	}
	if o.Sandbox != "" {
		params["sandbox"] = o.Sandbox
	}
	if o.Dir != "" {
		params["cwd"] = o.Dir
	}
	policy := o.ApprovalPolicy
	if policy == "" {
		policy = ApprovalNever
	}
	params["approvalPolicy"] = policy

	return params
}

// ApprovalNever tells the agent to act inside its sandbox and to fail rather
// than ask, because there is nobody to answer.
const ApprovalNever = "never"
