package policy_test

import (
	"context"
	"testing"

	"github.com/gnanam1990/argus/internal/computeruse/policy"
	"github.com/gnanam1990/argus/pkg/action"
)

func TestClassifyByTask(t *testing.T) {
	t.Parallel()
	c := policy.DefaultClassifier{}
	click := action.Action{Type: action.Click, Mark: action.NoMark}

	cases := []struct {
		task string
		want policy.RiskLevel
	}{
		{"change password for my email account", policy.HandOff},
		{"bypass the paywall on this article", policy.HandOff},
		{"delete all the photos in this album", policy.AlwaysConfirm},
		{"buy the item in my cart", policy.AlwaysConfirm},
		{"install this new app from the download", policy.AlwaysConfirm},
		{"post this comment on the thread", policy.AlwaysConfirm},
		{"solve the captcha to continue", policy.AlwaysConfirm},
		{"log in to my account", policy.PreApproval},
		{"upload the report to the site", policy.PreApproval},
		{"scroll down and read the article", policy.NoConfirm},
		{"click the play button", policy.NoConfirm},
	}
	for _, tc := range cases {
		got, reason := c.Classify(context.Background(), click, tc.task)
		if got != tc.want {
			t.Errorf("Classify(%q) = %v (%s), want %v", tc.task, got, reason, tc.want)
		}
	}
}

func TestClassifyGatedAndUntrusted(t *testing.T) {
	t.Parallel()
	c := policy.DefaultClassifier{}
	// A gated action with an innocuous task is still elevated.
	if got, _ := c.Classify(context.Background(), action.Action{Type: action.RunCommand, Text: "ls"}, "list files"); got != policy.AlwaysConfirm {
		t.Errorf("gated action = %v, want always_confirm", got)
	}
	// An untrusted click (values derived from on-screen content) is elevated.
	if got, _ := c.Classify(context.Background(), action.Action{Type: action.Click, Untrusted: true, Mark: action.NoMark}, "click ok"); got != policy.AlwaysConfirm {
		t.Errorf("untrusted action = %v, want always_confirm", got)
	}
}

func TestHasPreApproval(t *testing.T) {
	t.Parallel()
	// Both the data and destination named → pre-approved.
	if !policy.HasPreApproval("enter my email address on the newsletter form", "email", "newsletter") {
		t.Error("explicit data+destination should be pre-approved")
	}
	// Vague task → not pre-approved.
	if policy.HasPreApproval("fill in my details", "ssn", "irs.gov") {
		t.Error("a vague task must not count as pre-approval")
	}
	// Missing either side → false.
	if policy.HasPreApproval("send my email", "email", "") {
		t.Error("missing destination should be false")
	}
}

func TestRiskLevelString(t *testing.T) {
	t.Parallel()
	for lvl, want := range map[policy.RiskLevel]string{
		policy.NoConfirm:     "no_confirm",
		policy.PreApproval:   "pre_approval",
		policy.AlwaysConfirm: "always_confirm",
		policy.HandOff:       "hand_off",
	} {
		if lvl.String() != want {
			t.Errorf("%d.String() = %q, want %q", lvl, lvl.String(), want)
		}
	}
}
