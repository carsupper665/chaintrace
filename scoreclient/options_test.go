package scoreclient

import "testing"

func TestEnabledRequiresBothValues(t *testing.T) {
	cases := map[string]struct {
		options Options
		want    bool
	}{
		"absent":        {options: Options{}, want: false},
		"onlyBaseURL":   {options: Options{BaseURL: "http://127.0.0.1:7795"}, want: false},
		"onlySharedKey": {options: Options{SharedKey: "secret"}, want: false},
		"complete": {
			options: Options{BaseURL: "http://127.0.0.1:7795", SharedKey: "secret"},
			want:    true,
		},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			if got := test.options.Enabled(); got != test.want {
				t.Errorf("Enabled() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestNewReturnsNilWhenNotEnabled(t *testing.T) {
	if client := New(Options{}); client != nil {
		t.Errorf("New(Options{}) = %v, want nil", client)
	}
}
