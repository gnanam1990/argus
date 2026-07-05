package wayland

import "strings"

// keyCodes maps canonical key names (and single characters) to Linux
// input-event codes (linux/input-event-codes.h), which is what `ydotool key`
// consumes — unlike xdotool, ydotool speaks numeric keycodes, not keysym names.
// Names are matched case-insensitively.
var keyCodes = map[string]int{
	// Named keys.
	"esc": 1, "escape": 1,
	"minus": 12, "equal": 13, "backspace": 14, "tab": 15,
	"enter": 28, "return": 28,
	"ctrl": 29, "control": 29, "leftctrl": 29, "rightctrl": 97,
	"semicolon": 39, "apostrophe": 40, "grave": 41,
	"shift": 42, "leftshift": 42, "rightshift": 54, "backslash": 43,
	"comma": 51, "dot": 52, "period": 52, "slash": 53,
	"alt": 56, "leftalt": 56, "opt": 56, "option": 56, "rightalt": 100,
	"space": 57, "capslock": 58,
	"cmd": 125, "win": 125, "meta": 125, "super": 125, "leftmeta": 125, "rightmeta": 126,
	"home": 102, "up": 103, "pageup": 104, "left": 105, "right": 106,
	"end": 107, "down": 108, "pagedown": 109, "insert": 110, "delete": 111,
	"f1": 59, "f2": 60, "f3": 61, "f4": 62, "f5": 63, "f6": 64,
	"f7": 65, "f8": 66, "f9": 67, "f10": 68, "f11": 87, "f12": 88,
	// Digits.
	"1": 2, "2": 3, "3": 4, "4": 5, "5": 6, "6": 7, "7": 8, "8": 9, "9": 10, "0": 11,
	// Letters.
	"a": 30, "b": 48, "c": 46, "d": 32, "e": 18, "f": 33, "g": 34, "h": 35, "i": 23,
	"j": 36, "k": 37, "l": 38, "m": 50, "n": 49, "o": 24, "p": 25, "q": 16, "r": 19,
	"s": 31, "t": 20, "u": 22, "v": 47, "w": 17, "x": 45, "y": 21, "z": 44,
}

// keyCode resolves a key name to its Linux input-event code.
func keyCode(k string) (int, bool) {
	c, ok := keyCodes[strings.ToLower(k)]
	return c, ok
}
