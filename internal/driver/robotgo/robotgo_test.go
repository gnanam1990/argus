//go:build robotgo

package robotgo

import "testing"

// TestScrollArgsNegatesBothAxes locks in H1: robotgo.Scroll runs the opposite
// sign convention from our canonical DX/DY (positive DY down, positive DX
// right), on both axes, so scrollArgs must negate both.
func TestScrollArgsNegatesBothAxes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		dx, dy       int
		wantX, wantY int
	}{
		{0, 5, 0, -5},  // canonical down  -> robotgo negative y
		{0, -5, 0, 5},  // canonical up    -> robotgo positive y
		{5, 0, -5, 0},  // canonical right -> robotgo negative x
		{-5, 0, 5, 0},  // canonical left  -> robotgo positive x
		{3, 4, -3, -4}, // both axes at once
		{0, 0, 0, 0},   // no-op
	}
	for _, c := range cases {
		x, y := scrollArgs(c.dx, c.dy)
		if x != c.wantX || y != c.wantY {
			t.Errorf("scrollArgs(%d,%d) = (%d,%d), want (%d,%d)", c.dx, c.dy, x, y, c.wantX, c.wantY)
		}
	}
}

// TestTranslateKeyAliases covers the H2 alias set: win/meta collapse to
// robotgo's own "cmd" (which already resolves to the correct per-OS meta key
// internally), option/opt collapse to "alt", and return folds to "enter"
// since robotgo has no "return" entry at all. Aliases are matched
// case-insensitively.
func TestTranslateKeyAliases(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"win", "cmd"},
		{"WIN", "cmd"},
		{"Win", "cmd"},
		{"meta", "cmd"},
		{"META", "cmd"},
		{"option", "alt"},
		{"opt", "alt"},
		{"OPT", "alt"},
		{"return", "enter"},
		{"Return", "enter"},
	}
	for _, c := range cases {
		got, err := translateKey(c.in)
		if err != nil {
			t.Errorf("translateKey(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("translateKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestTranslateKeyKnownNamesPassThrough covers names already present in
// robotgo v1.0.2's keyNames map (key.go:209-326): they must pass through
// (lowercased) rather than being rejected.
func TestTranslateKeyKnownNamesPassThrough(t *testing.T) {
	t.Parallel()
	names := []string{
		"enter", "esc", "escape", "tab", "space", "backspace", "delete",
		"up", "down", "left", "right", "home", "end", "pageup", "pagedown",
		"cmd", "alt", "ctrl", "shift", "control", "f1", "f12",
	}
	for _, k := range names {
		got, err := translateKey(k)
		if err != nil {
			t.Errorf("translateKey(%q) unexpected error: %v", k, err)
			continue
		}
		if got != k {
			t.Errorf("translateKey(%q) = %q, want unchanged %q", k, got, k)
		}
	}
	// Case-insensitive passthrough still normalizes to robotgo's (lowercase)
	// spelling, since checkKeyCodes does an exact, case-sensitive map lookup.
	if got, err := translateKey("ENTER"); err != nil || got != "enter" {
		t.Errorf(`translateKey("ENTER") = %q, %v, want "enter", nil`, got, err)
	}
}

// TestTranslateKeySingleCharPassesThroughUnchanged covers robotgo's other
// valid-key path (keyCodeForChar), which is case-sensitive — "a" and "A" are
// different keys (shift is implied by case) — so single characters must
// never be lowercased or otherwise altered.
func TestTranslateKeySingleCharPassesThroughUnchanged(t *testing.T) {
	t.Parallel()
	for _, k := range []string{"a", "A", "1", "!", "Z"} {
		got, err := translateKey(k)
		if err != nil {
			t.Errorf("translateKey(%q) unexpected error: %v", k, err)
			continue
		}
		if got != k {
			t.Errorf("translateKey(%q) = %q, want unchanged %q", k, got, k)
		}
	}
}

// TestTranslateKeyUnknownErrors is the core H2 regression: a name that is
// neither a known alias, a single character, nor a robotgo keyNames member
// must be a hard error, never a silent fallthrough to keycode 0 (kVK_ANSI_A,
// i.e. "a", on macOS).
func TestTranslateKeyUnknownErrors(t *testing.T) {
	t.Parallel()
	for _, k := range []string{"super", "hyper", "bogus", "windows", "commandkey", ""} {
		got, err := translateKey(k)
		if err == nil {
			t.Errorf("translateKey(%q) = %q, nil error; want an unknown-key error", k, got)
		}
	}
}

// TestTranslateKeysAppliesAcrossTheWholeChord ensures every element of a
// chord is translated, not just the trailing "key" position — a modifier
// like "win" is just as capable of silently misbehaving as the primary key.
func TestTranslateKeysAppliesAcrossTheWholeChord(t *testing.T) {
	t.Parallel()
	got, err := translateKeys([]string{"win", "shift", "s"})
	if err != nil {
		t.Fatalf("translateKeys: %v", err)
	}
	want := []string{"cmd", "shift", "s"}
	if len(got) != len(want) {
		t.Fatalf("translateKeys = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("translateKeys[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestTranslateKeysPropagatesFirstError ensures an unknown key anywhere in
// the chord — including a modifier position — fails the whole chord instead
// of silently proceeding with a partially-translated set.
func TestTranslateKeysPropagatesFirstError(t *testing.T) {
	t.Parallel()
	if _, err := translateKeys([]string{"ctrl", "nonsense"}); err == nil {
		t.Error("expected an error for an unknown key in the chord")
	}
	if _, err := translateKeys([]string{"nonsense", "a"}); err == nil {
		t.Error("expected an error for an unknown modifier in the chord")
	}
}

// TestMoveSettleIsPositive locks in the dropped-click fix: robotgo.Move warps
// the cursor asynchronously, so a button event posted in the same breath lands
// at the pre-move location and macOS drops it (the pointer visibly reaches the
// target yet nothing is pressed). moveTo waits moveSettleMS between the warp
// and the event to avoid that race; a zero settle reintroduces the bug, so this
// guards against anyone tuning it back down to nothing. Live testing showed
// 40ms sufficient; the constant keeps a margin above that.
func TestMoveSettleIsPositive(t *testing.T) {
	t.Parallel()
	if moveSettleMS < 40 {
		t.Fatalf("moveSettleMS = %d; must stay >= 40ms or synthetic clicks get dropped after a move", moveSettleMS)
	}
}

// TestDisplayOffset verifies the local->global coordinate mapping used for
// multi-monitor input: a driver bound to a display with a non-zero origin adds
// that origin to every input coordinate. The display bounds are injected so the
// test needs no real hardware.
func TestDisplayOffset(t *testing.T) {
	t.Parallel()
	// A driver bound to a secondary display at global origin (4480,0).
	d := &Driver{display: 2, bounds: func(int) (int, int, int, int) { return 4480, 0, 1920, 1080 }}

	if gx, gy := d.g(960, 540); gx != 5440 || gy != 540 {
		t.Errorf("g(960,540) = (%d,%d), want (5440,540)", gx, gy)
	}
	// Primary display (origin 0,0) is a pass-through.
	p := &Driver{display: 0, bounds: func(int) (int, int, int, int) { return 0, 0, 2560, 1080 }}
	if gx, gy := p.g(100, 200); gx != 100 || gy != 200 {
		t.Errorf("primary g(100,200) = (%d,%d), want (100,200)", gx, gy)
	}
}

// TestOriginResolvedLive locks in that the display origin is read on every call
// (not cached at construction), so a mid-run display rearrange is picked up: g()
// must reflect a bounds change without rebuilding the driver.
func TestOriginResolvedLive(t *testing.T) {
	t.Parallel()
	origin := 4480
	d := &Driver{display: 1, bounds: func(int) (int, int, int, int) { return origin, 0, 1920, 1080 }}
	if gx, _ := d.g(10, 0); gx != 4490 {
		t.Fatalf("g before move = %d, want 4490", gx)
	}
	origin = 2560 // the display got rearranged under us
	if gx, _ := d.g(10, 0); gx != 2570 {
		t.Errorf("g after rearrange = %d, want 2570 (origin must be re-resolved live)", gx)
	}
}
