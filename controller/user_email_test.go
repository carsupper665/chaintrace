package controller

import (
	"strings"
	"testing"
	"time"
)

// The stated expiry has to come from the challenge's own TTL. A number typed
// into the copy drifts the moment ChallengeTTL is configured, and an email that
// promises five minutes on a two-minute link is worse than one that says
// nothing.
func TestVerificationEmailStatesTheRealExpiry(t *testing.T) {
	for _, ttl := range []time.Duration{2 * time.Minute, 5 * time.Minute, 15 * time.Minute} {
		subject, body := verificationEmail("owner", "https://backend.test/verify?code=a&id=1", ttl)
		want := strings.ReplaceAll(ttl.String(), "m0s", " 分鐘")
		minutes := want[:strings.Index(want, " ")]
		if !strings.Contains(subject, minutes+" 分鐘") {
			t.Errorf("subject %q does not state %s minutes", subject, minutes)
		}
		if !strings.Contains(body, minutes+" 分鐘後失效") {
			t.Errorf("body does not state %s minutes for ttl %s", minutes, ttl)
		}
	}
}

// A sub-minute TTL must not round down to "0 分鐘後失效".
func TestVerificationEmailNeverPromisesZeroMinutes(t *testing.T) {
	subject, body := verificationEmail("owner", "https://backend.test/verify", 20*time.Second)
	if strings.Contains(subject, "0 分鐘") || strings.Contains(body, "0 分鐘") {
		t.Errorf("sub-minute TTL rendered as zero minutes: %q", subject)
	}
}

// The Owner's username reaches HTML. It is chosen by whoever registered, so it
// is untrusted input in this context.
func TestVerificationEmailEscapesTheUsername(t *testing.T) {
	_, body := verificationEmail(`<script>alert(1)</script>`, "https://backend.test/verify", time.Minute)
	if strings.Contains(body, "<script>") {
		t.Error("username was interpolated into the email unescaped")
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Error("escaped username is missing from the email")
	}
}

// Clients rewrite or strip the button often enough that a link-only email locks
// people out, so the URL also appears as text — and its query separator has to
// survive as a real ampersand once the client parses the HTML.
func TestVerificationEmailCarriesTheURLAsTextAndHref(t *testing.T) {
	url := "https://backend.test/Authentication/verify?code=abc&id=42"
	_, body := verificationEmail("owner", url, time.Minute)
	escaped := "https://backend.test/Authentication/verify?code=abc&amp;id=42"
	if strings.Count(body, escaped) < 2 {
		t.Errorf("expected the URL in both the href and the visible text, body: %s", body)
	}
	if strings.Contains(body, url) {
		t.Error("the raw & was left unescaped in HTML")
	}
}
