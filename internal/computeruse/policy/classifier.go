// Package policy classifies a proposed UI action into a confirmation risk
// level, so the confirmation middleware knows whether to allow it, ask the
// user, or hand the task back. It is a heuristic backstop that leans toward
// caution — the authoritative safety layers are the human approval gate and the
// model-facing computer-use-safety skill; this catches obviously risky intent
// even if the model ignores its guidance.
package policy

import (
	"context"
	"strings"

	"github.com/gnanam1990/argus/pkg/action"
)

// RiskLevel is how much confirmation an action needs, from none to hand-off.
type RiskLevel int

const (
	// NoConfirm: routine UI interaction in an approved app — allow.
	NoConfirm RiskLevel = iota
	// PreApproval: allowed only if the user's task explicitly authorized it,
	// otherwise ask.
	PreApproval
	// AlwaysConfirm: ask the user immediately before acting, every time.
	AlwaysConfirm
	// HandOff: the agent should not do this at all; the user must take over.
	HandOff
)

// String renders a RiskLevel.
func (r RiskLevel) String() string {
	switch r {
	case PreApproval:
		return "pre_approval"
	case AlwaysConfirm:
		return "always_confirm"
	case HandOff:
		return "hand_off"
	default:
		return "no_confirm"
	}
}

// ActionClassifier assigns a risk level (and a human-readable reason) to a
// proposed action in the context of the user's task.
type ActionClassifier interface {
	Classify(ctx context.Context, a action.Action, task string) (RiskLevel, string)
}

// category groups the keyword signals for one risk level.
type category struct {
	level    RiskLevel
	reason   string
	keywords []string
}

// categories are checked highest-risk first. Keywords are matched
// case-insensitively against the task text. These are standard high-risk
// computer-use categories (destructive, credential, financial, communication,
// security-bypass), not anyone's proprietary list.
var categories = []category{
	{HandOff, "changing a password or bypassing a security/paywall barrier", []string{
		"change password", "reset password", "bypass paywall", "bypass the paywall",
		"bypass security", "not secure warning", "captcha bypass",
	}},
	{AlwaysConfirm, "a destructive, credential, financial, install, or third-party-communication action", []string{
		"delete", "permanently remove", "erase", "wipe",
		"create account", "sign up", "register an account",
		"api key", "oauth", "access token", "save password", "save card", "save credit card",
		"captcha",
		"install", "run the downloaded", "run downloaded", "browser extension",
		"post", "comment", "reply", "publish", "tweet", "send message", "send email", "dm ",
		"appointment", "reservation", "book a", "subscribe", "unsubscribe",
		"pay", "purchase", "buy", "checkout", "transfer money", "wire ",
		"vpn", "firewall", "security setting", "system setting",
		"medical", "prescription",
	}},
	{PreApproval, "logging in, accepting a permission/warning, uploading, or transmitting sensitive data", []string{
		"log in", "login", "sign in", "sign-in",
		"permission prompt", "allow access", "age verification", "accept the warning",
		"upload", "rename", "move file",
		"enter password", "enter otp", "one-time code", "credit card", "ssn", "social security",
	}},
}

// DefaultClassifier scans the task text for risk-category keywords and elevates
// the level for gated or untrusted actions. Ordinary clicks/typing/scrolling in
// an approved app are NoConfirm.
type DefaultClassifier struct{}

var _ ActionClassifier = DefaultClassifier{}

// Classify returns the risk level and reason for a in the context of task.
func (DefaultClassifier) Classify(_ context.Context, a action.Action, task string) (RiskLevel, string) {
	lt := strings.ToLower(task)
	for _, c := range categories {
		for _, kw := range c.keywords {
			if strings.Contains(lt, kw) {
				return c.level, c.reason
			}
		}
	}
	// Gated actions (run_command / file ops / window control) are sensitive
	// regardless of task wording; an action derived from untrusted on-screen
	// content is likewise not to be trusted silently.
	if a.Type.Gated() || a.Untrusted {
		return AlwaysConfirm, "a gated or untrusted action"
	}
	return NoConfirm, "routine interaction"
}

// HasPreApproval reports whether the task explicitly authorized transmitting
// the given sensitive data to the given destination. Both must be named — a
// vague task ("fill in my details") is not pre-approval for sending an SSN to a
// third-party site.
func HasPreApproval(task, sensitiveData, destination string) bool {
	if sensitiveData == "" || destination == "" {
		return false
	}
	lt := strings.ToLower(task)
	return strings.Contains(lt, strings.ToLower(sensitiveData)) &&
		strings.Contains(lt, strings.ToLower(destination))
}
