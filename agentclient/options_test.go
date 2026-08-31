package agentclient

import "testing"

// A half-finished configuration is the failure that looks like a healthy
// "agent is down": the sidecar is never contacted and every turn degrades.
// It must never be silent.
func TestHalfConfiguredAgentIsReported(t *testing.T) {
	cases := map[string]struct {
		options Options
		want    string
	}{
		"onlyBaseURL": {
			options: Options{BaseURL: "http://127.0.0.1:7795"},
			want:    "AGENT_BASE_URL is set but AGENT_SHARED_KEY is empty",
		},
		"onlySharedKey": {
			options: Options{SharedKey: "secret"},
			want:    "AGENT_SHARED_KEY is set but AGENT_BASE_URL is empty",
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			if got := test.options.ConfigurationWarning(); got != test.want {
				t.Errorf("warning = %q, want %q", got, test.want)
			}
			if test.options.Enabled() {
				t.Error("a half-configured Agent must stay disabled")
			}
		})
	}
}

// Deliberately running without an Agent is normal and must stay quiet.
func TestFullyConfiguredOrAbsentAgentIsQuiet(t *testing.T) {
	for name, options := range map[string]Options{
		"absent":   {},
		"complete": {BaseURL: "http://127.0.0.1:7795", SharedKey: "secret"},
	} {
		t.Run(name, func(t *testing.T) {
			if warning := options.ConfigurationWarning(); warning != "" {
				t.Errorf("warning = %q, want none", warning)
			}
		})
	}
}
