// Package agentclient talks to the Python LLM Agent sidecar and executes the
// tool calls it asks for.
//
// The boundary is one-directional: this package builds evidence, sends it to
// the Agent, and runs whatever tool the Agent requests — after checking that
// the request stays inside the Owner's current Analysis Dataset. The Agent
// never reaches the database and never sees an Owner identity.
//
// See docs/development-rules.md sections 2, 3 and 6.
package agentclient

import (
	"time"

	"chaintrace/utils"
)

// Options is the deployment configuration for the Agent sidecar. A zero
// BaseURL disables the Agent, which is how the HTTP tests and any deployment
// without a sidecar keep the existing agent_unavailable behaviour.
type Options struct {
	// BaseURL is the sidecar's origin, e.g. http://127.0.0.1:7795. The sidecar
	// binds a LAN or loopback address and is never reachable from the browser.
	BaseURL string
	// SharedKey goes out as X-Agent-Key. It proves the caller is this Go API;
	// it is not an authorization mechanism.
	SharedKey string
	// HTTPTimeout bounds one call to the sidecar. Exceeding it degrades the
	// turn to agent_unavailable.
	HTTPTimeout time.Duration
	// MaxToolCalls caps how many Tier 1 tools one turn may run. On reaching the
	// cap the turn is answered from the evidence already gathered.
	MaxToolCalls int
	// TurnTimeout is the wall-clock budget for a whole turn, tool round trips
	// included.
	TurnTimeout time.Duration
	// ToolMaxRows caps the rows a single tool call may return so one call
	// cannot exhaust the model's context.
	ToolMaxRows int
}

func ProductionOptions() Options {
	return Options{
		BaseURL:      utils.GetEnvString("AGENT_BASE_URL", ""),
		SharedKey:    utils.GetEnvString("AGENT_SHARED_KEY", ""),
		HTTPTimeout:  time.Duration(utils.GetEnvInt("AGENT_HTTP_TIMEOUT_MS", 30000)) * time.Millisecond,
		MaxToolCalls: utils.GetEnvInt("AGENT_MAX_TOOL_CALLS", 5),
		TurnTimeout:  time.Duration(utils.GetEnvInt("AGENT_TURN_TIMEOUT_MS", 60000)) * time.Millisecond,
		ToolMaxRows:  utils.GetEnvInt("AGENT_TOOL_MAX_ROWS", 200),
	}
}

// Enabled reports whether an Agent is configured. Both values are required:
// an unauthenticated sidecar would accept requests from anything on the LAN.
func (o Options) Enabled() bool {
	return o.BaseURL != "" && o.SharedKey != ""
}

// ConfigurationWarning describes a half-finished Agent configuration.
//
// Setting one variable and not the other is a mistake worth saying out loud:
// the Agent stays off and every conversation answers agent_unavailable, which
// looks identical to a sidecar that is down. It returns "" when the Agent is
// fully configured and when it is deliberately absent.
func (o Options) ConfigurationWarning() string {
	switch {
	case o.BaseURL != "" && o.SharedKey == "":
		return "AGENT_BASE_URL is set but AGENT_SHARED_KEY is empty"
	case o.BaseURL == "" && o.SharedKey != "":
		return "AGENT_SHARED_KEY is set but AGENT_BASE_URL is empty"
	default:
		return ""
	}
}

// withDefaults fills in the bounds a caller left at zero so a hand-built
// Options in a test cannot accidentally disable every limit.
func (o Options) withDefaults() Options {
	if o.HTTPTimeout <= 0 {
		o.HTTPTimeout = 30 * time.Second
	}
	if o.MaxToolCalls <= 0 {
		o.MaxToolCalls = 5
	}
	if o.TurnTimeout <= 0 {
		o.TurnTimeout = 60 * time.Second
	}
	if o.ToolMaxRows <= 0 {
		o.ToolMaxRows = 200
	}
	return o
}
