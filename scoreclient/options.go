// Package scoreclient calls the Python Agent sidecar's POST /v1/score route —
// the learned risk scorer (docs/adr/0015-learned-risk-scoring-alongside-rules.md).
//
// It is a separate, small package from agentclient rather than folded into
// it: agentclient speaks the chat/tool-loop protocol to the same sidecar
// process, an unrelated and much larger concern. This package's only job is
// one request, one response.
package scoreclient

import (
	"time"

	"chaintrace/utils"
)

// Options configures the call to the sidecar's scoring route. A zero BaseURL
// disables it, mirroring agentclient.Options — no scorer configured is a
// normal, quiet state, not a misconfiguration.
type Options struct {
	// BaseURL is the sidecar's origin. Same process, same origin as the Agent
	// chat sidecar (ADR-0015: "The model lives in its own package inside the
	// Agent server process").
	BaseURL string
	// SharedKey goes out as X-Agent-Key, the same shared secret as agentclient
	// uses — it proves the caller is this Go API, not an authorization scheme.
	SharedKey string
	// HTTPTimeout bounds one call to the sidecar's scoring route.
	HTTPTimeout time.Duration
}

func ProductionOptions() Options {
	return Options{
		BaseURL:     utils.GetEnvString("AGENT_BASE_URL", ""),
		SharedKey:   utils.GetEnvString("AGENT_SHARED_KEY", ""),
		HTTPTimeout: time.Duration(utils.GetEnvInt("SCORING_HTTP_TIMEOUT_MS", 30000)) * time.Millisecond,
	}
}

// Enabled reports whether a scorer is configured. Both values are required,
// same reasoning as agentclient.Options.Enabled: an unauthenticated sidecar
// would accept requests from anything on the LAN.
func (o Options) Enabled() bool {
	return o.BaseURL != "" && o.SharedKey != ""
}

func (o Options) withDefaults() Options {
	if o.HTTPTimeout <= 0 {
		o.HTTPTimeout = 30 * time.Second
	}
	return o
}
