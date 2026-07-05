package ax

// HostSource reads the accessibility tree of the real, frontmost macOS
// application via `osascript -l JavaScript` (JXA) driving System Events. It
// is the only TreeSource Argus ships: it needs no CGo, so this file carries
// no build tag and compiles on every OS, and it reports ErrUnavailable at
// runtime wherever the tree can't actually be read — not darwin, osascript
// missing, a timeout, or missing Accessibility permission — so a chain
// grounder falls back to a vision detector.
//
// Coordinate spaces: System Events reports every element's position and size
// in the main display's logical POINT space, while a screenshot can be in a
// different PIXEL space — e.g. the robotgo driver on a Retina/HiDPI Mac
// captures physical pixels, 2x (or 3x) the point size. detect scales every
// box by (screenshot pixel size / main-display point size) before returning,
// so boxes always land in the screenshot-pixel space grounder.Element
// requires. The point size comes from the same JXA run, read via
// NSScreen.mainScreen.frame through the ObjC bridge — a plain AppKit query
// that (unlike the element walk) needs no Accessibility permission, so it
// works even when the rest of the payload reports zero elements.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // register the JPEG decoder; action.Image may carry either MIME type
	_ "image/png"  // register the PNG decoder
	"math"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/gnanam1990/argus/pkg/action"
	"github.com/gnanam1990/argus/pkg/grounder"
)

// Runner executes an external command and returns its stdout. It mirrors
// internal/driver/shell's Runner so every test in this package feeds fixture
// output through a fake and never spawns a real process.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// ExecRunner runs commands with os/exec. osascript only exists on macOS, so
// Run fails fast on any other GOOS instead of attempting to spawn it — this
// keeps the "wrong platform" case cheap and its error message specific,
// without requiring hostSource itself to know or care which OS it's on (a
// fake Runner injected for tests is under no such restriction).
type ExecRunner struct{}

// Run executes name with args and returns its stdout.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("ax: %s is macOS-only, unsupported on %s", name, runtime.GOOS)
	}
	return exec.CommandContext(ctx, name, args...).Output()
}

const (
	// defaultTimeout bounds a single osascript run so a wedged System Events
	// call — e.g. a permission prompt no one can click in a headless or
	// remote session — can't stall a step forever; Detect returns
	// ErrUnavailable instead and the chain grounder falls back to vision.
	defaultTimeout = 5 * time.Second

	// maxDepth and maxElements bound the JXA walk for latency: an unbounded
	// walk of a deep or huge UI tree (e.g. a complex web page inside
	// AXWebArea) could otherwise make a single Detect call take seconds.
	maxDepth    = 8
	maxElements = 400
)

// interactableRoles are the AXRole values that resolve to a clickable or
// settable control. Roles left out are either purely structural (AXGroup,
// AXWindow, AXStaticText used as a label — still walked for their children,
// just never marked Interactable) or, for AXTabGroup specifically, a
// container whose bounding box isn't itself a meaningful click target: its
// individual tabs surface as children (typically AXRadioButton, sometimes
// AXTab) and are covered by the roles below.
var interactableRoles = map[string]bool{
	"AXButton":             true,
	"AXTextField":          true,
	"AXTextArea":           true,
	"AXCheckBox":           true,
	"AXRadioButton":        true,
	"AXPopUpButton":        true,
	"AXComboBox":           true,
	"AXLink":               true,
	"AXMenuItem":           true,
	"AXMenuButton":         true,
	"AXTab":                true,
	"AXSlider":             true,
	"AXIncrementor":        true,
	"AXDisclosureTriangle": true,
}

// HostOption configures HostSource.
type HostOption func(*hostSource)

// WithRunner overrides the command runner (for tests). Injecting a runner also
// disables the native (cgo) walk so the test drives the osascript path
// deterministically through the fake.
func WithRunner(r Runner) HostOption {
	return func(h *hostSource) { h.run = r; h.native = false }
}

// WithTimeout overrides the default 5s osascript timeout.
func WithTimeout(d time.Duration) HostOption { return func(h *hostSource) { h.timeout = d } }

// WithLogicalCoords keeps element frames in logical screen points (the space
// the accessibility API and the input driver both use) instead of scaling them
// into the screenshot's pixel space. The set-of-marks vision grounder wants
// pixel space so marks line up with the screenshot it sends a model; the
// computer-use path instead feeds these frames straight to the driver's
// click/scroll, which expects logical points — scaling them (2x on a Retina
// display) would land every click at roughly twice the intended offset. Display
// offset and off-display filtering still apply; only the pixel scaling is
// suppressed.
func WithLogicalCoords() HostOption { return func(h *hostSource) { h.logical = true } }

// WithDisplayBounds binds the tree source (and a Clicker built with the same
// option) to a specific display given its global bounds in logical points
// (x,y = top-left origin; w,h = size), as reported by the driver. Element
// frames — which the accessibility API reports in whole-desktop global
// coordinates — are then offset into that display's local space and scaled by
// its size, and elements on other displays are filtered out, so marks align
// with a per-display screenshot. Unset (the zero value) keeps the primary /
// whole-desktop behavior.
func WithDisplayBounds(x, y, w, h int) HostOption {
	return func(hs *hostSource) {
		hs.dispX, hs.dispY, hs.dispW, hs.dispH = float64(x), float64(y), float64(w), float64(h)
	}
}

type hostSource struct {
	run     Runner
	timeout time.Duration
	native  bool // try the cgo AXUIElement walk before osascript
	logical bool // keep frames in logical points (no screenshot-pixel scaling)

	// display bounds in logical points (0,0,0,0 = unset → whole-desktop).
	dispX, dispY, dispW, dispH float64
}

// errNativeUnavailable means the cgo AXUIElement walk isn't usable (non-cgo
// build, non-darwin, or the process isn't accessibility-trusted); the tree
// source falls back to the osascript walk.
var errNativeUnavailable = errors.New("ax: native accessibility walk unavailable")

// FrontmostBundleID returns the bundle identifier of the application currently
// frontmost, or "" when it can't be determined (including on non-darwin or
// non-cgo builds, where callers should treat it as "unverifiable"). The native
// accessibility walk reads whichever app is frontmost, so callers verify with
// this that the intended app is actually in front before trusting the walk.
func FrontmostBundleID() string { return nativeFrontmostBundle() }

// HostSource returns a TreeSource backed by the real host's accessibility
// tree (see the package doc above for the JXA approach and coordinate
// mapping). Wire it in with ax.New(ax.WithSource(ax.HostSource())).
func HostSource(opts ...HostOption) TreeSource {
	h := &hostSource{run: ExecRunner{}, timeout: defaultTimeout, native: true}
	for _, o := range opts {
		o(h)
	}
	return h.detect
}

// detect runs the JXA script, parses its single JSON payload, and scales the
// walked elements into the screenshot's pixel space.
func (h *hostSource) detect(ctx context.Context, img action.Image) ([]grounder.Element, error) {
	// Prefer the native cgo AXUIElement walk: it needs no Automation grant and
	// cannot hang the way the System Events JXA walk can. Fall back to
	// osascript when it isn't available (non-cgo build, or not trusted).
	if h.native {
		if screen, els, err := nativeWalk(); err == nil {
			return h.finish(screen, els, img), nil
		}
	}

	cctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	out, err := h.run.Run(cctx, "osascript", "-l", "JavaScript", "-e", jxaScript)
	if err != nil {
		return nil, runError(err)
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return nil, fmt.Errorf("ax: osascript produced no output: %w", ErrUnavailable)
	}
	screen, els, err := parseTree(out)
	if err != nil {
		return nil, fmt.Errorf("ax: parse osascript output: %w: %v", ErrUnavailable, err)
	}
	return h.finish(screen, els, img), nil
}

// finish scales walked elements (from either the native or osascript walk) into
// the screenshot's pixel space. When display bounds are set it grounds against
// that display: offset global frames into its local space, scale by its size,
// and filter out elements on other monitors; otherwise it uses the reported
// primary-screen size.
func (h *hostSource) finish(screen wireScreen, els []wireElement, img action.Image) []grounder.Element {
	refW, refH := screen.W, screen.H
	ox, oy := 0.0, 0.0
	filter := false
	if h.dispW > 0 && h.dispH > 0 {
		refW, refH = h.dispW, h.dispH
		ox, oy = h.dispX, h.dispY
		filter = true
	}
	sx, sy := 1.0, 1.0
	// In logical mode the frames are consumed by the input driver, which works
	// in logical points, so leave scale at 1 (AX frames are already logical).
	if !h.logical {
		if imgW, imgH, ok := decodeSize(img); ok && refW > 0 && refH > 0 {
			sx = float64(imgW) / refW
			sy = float64(imgH) / refH
		}
	}
	return mapElements(els, sx, sy, ox, oy, refW, refH, filter)
}

// runError classifies a failed osascript invocation into an
// ErrUnavailable-wrapped error. Both branches wrap ErrUnavailable — so
// errors.Is(err, ErrUnavailable) is true either way and a chain grounder
// falls back to vision regardless of which one fired — but the
// assistive-access branch additionally names the fix, since that cause is
// both common and self-resolvable.
func runError(err error) error {
	msg := err.Error()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
		// exec.Cmd.Output's default *ExitError.Error() is just "exit status
		// N"; the process's actual complaint (what we need to pattern-match
		// below) lives in the captured Stderr instead.
		msg = string(exitErr.Stderr)
	}
	if isAssistiveDenied(msg) {
		return fmt.Errorf("ax: accessibility permission denied (osascript: %s) — grant "+
			"Accessibility to the terminal/app that launched argus (System Settings > "+
			"Privacy & Security > Accessibility), then relaunch it: %w",
			strings.TrimSpace(msg), ErrUnavailable)
	}
	return fmt.Errorf("ax: osascript failed (%v) — often the same missing Accessibility "+
		"permission (System Settings > Privacy & Security > Accessibility) surfacing as a "+
		"timeout rather than a clean denial: %w", err, ErrUnavailable)
}

// isAssistiveDenied reports whether msg names one of the known signals for
// "this process is not authorized to read another app's accessibility tree".
func isAssistiveDenied(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "assistive access") ||
		strings.Contains(msg, "-25211") ||
		strings.Contains(msg, "1002")
}

// wireScreen is the JXA payload's main-display point size.
type wireScreen struct {
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// wireElement is one walked accessibility element, still in logical/point
// space, exactly as the JXA script (see jxaScriptTemplate) emits it.
type wireElement struct {
	Role    string  `json:"role"`
	Title   string  `json:"title"`
	Desc    string  `json:"desc"`
	Value   string  `json:"value"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	W       float64 `json:"w"`
	H       float64 `json:"h"`
	Enabled bool    `json:"enabled"`
}

// wirePayload is the single JSON object jxaScript returns on stdout.
type wirePayload struct {
	Screen   *wireScreen   `json:"screen"`
	Elements []wireElement `json:"elements"`
}

// parseTree decodes the JXA payload. Invalid JSON, or JSON missing the
// screen record, is a hard error rather than something silently coerced into
// "0 elements" — a truncated or corrupted run should surface as a failure so
// the chain grounder falls back, not degrade quietly.
func parseTree(out []byte) (wireScreen, []wireElement, error) {
	var payload wirePayload
	if err := json.Unmarshal(out, &payload); err != nil {
		return wireScreen{}, nil, fmt.Errorf("decode json: %w", err)
	}
	if payload.Screen == nil {
		return wireScreen{}, nil, fmt.Errorf("missing screen record")
	}
	return *payload.Screen, payload.Elements, nil
}

// mapElements converts wire elements (global logical/point space) into
// grounder.Element (screenshot-pixel space): it offsets each element's frame
// into the reference display's local space by (ox, oy), scales by (sx, sy),
// classifies Interactable from role + enabled, and falls back to Value for
// Label when Title is empty. When filter is set, elements that fall entirely
// outside the reference display [0,refW]×[0,refH] (they live on another
// monitor) are dropped. IDs are assigned sequentially in walk order —
// grounder.Renumber exists for callers that later compose multiple sources, so
// this package doesn't need to deduplicate. Elements that resolve to a
// zero-area box (all attribute reads failed, or a genuinely zero-size/hidden
// element) are dropped: they're not a usable set-of-marks target either way.
func mapElements(els []wireElement, sx, sy, ox, oy, refW, refH float64, filter bool) []grounder.Element {
	out := make([]grounder.Element, 0, len(els))
	for _, e := range els {
		lx, ly := e.X-ox, e.Y-oy // global point -> display-local point
		if filter && (lx+e.W <= 0 || lx >= refW || ly+e.H <= 0 || ly >= refH) {
			continue // fully off this display
		}
		box := action.Rect{
			Min: action.Point{X: round(lx * sx), Y: round(ly * sy)},
			Max: action.Point{X: round((lx + e.W) * sx), Y: round((ly + e.H) * sy)},
		}
		if box.Empty() {
			continue
		}
		label := e.Title
		if label == "" {
			label = e.Desc // icon buttons often label via AXDescription, not AXTitle
		}
		if label == "" {
			label = e.Value
		}
		out = append(out, grounder.Element{
			ID:           len(out),
			Box:          box,
			Label:        label,
			Text:         e.Value,
			Interactable: interactableRoles[e.Role] && e.Enabled,
			Confidence:   1.0,
		})
	}
	return out
}

func round(f float64) int { return int(math.Round(f)) }

// decodeSize reads an encoded image's pixel dimensions without a full pixel
// decode (mirrors pkg/agent's private decodeSize helper: DecodeConfig only
// reads the header).
func decodeSize(img action.Image) (w, h int, ok bool) {
	if img.Empty() {
		return 0, 0, false
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(img.Data))
	if err != nil {
		return 0, 0, false
	}
	return cfg.Width, cfg.Height, true
}

// jxaScriptTemplate is a JXA (`osascript -l JavaScript`) program. It:
//  1. Reads the main display's logical point size via the ObjC/AppKit
//     bridge (NSScreen.mainScreen.frame) — this needs no Accessibility
//     permission, so the screen size is still reported even when the walk
//     below fails or is denied.
//  2. Finds the frontmost application process via System Events and walks
//     its UI element tree depth-first (bounded by maxDepth/maxElements,
//     substituted below), reading each element's role/title/value/enabled
//     and position/size. Every attribute read is wrapped in try/catch: many
//     elements throw for attributes they don't support, and one such element
//     must not abort the whole walk.
//  3. Returns one JSON object (screen size + the walked elements) as a
//     string. Returning it — rather than printing via console.log, which
//     this osascript build sends to stderr, not stdout — keeps stdout exactly
//     the single JSON blob callers expect, on the same "capture stdout via
//     exec...Output()" convention as this package's Runner and
//     internal/driver/shell's.
//
// The applicationProcesses.whose({frontmost:true})[0] call is deliberately
// left outside any try/catch: if the calling process lacks Accessibility (or
// Automation) permission, System Events raises an error here (or the call
// hangs until Detect's own context timeout kills the whole process) — either
// way that failure must propagate so osascript exits non-zero and Go's
// runError can classify it, instead of this script silently swallowing it
// and reporting a misleading "0 elements found".
const jxaScriptTemplate = `function run() {
  var maxDepth = %d;
  var maxElements = %d;

  function attr(el, method, axName) {
    try {
      var v = el[method]();
      if (v !== null && v !== undefined) { return v; }
    } catch (e1) {}
    try {
      var v2 = el.attributes.byName(axName).value();
      if (v2 !== null && v2 !== undefined) { return v2; }
    } catch (e2) {}
    return null;
  }

  function str(v) {
    if (v === null || v === undefined) { return ""; }
    return "" + v;
  }

  var screenW = 0;
  var screenH = 0;
  try {
    ObjC.import("AppKit");
    var frame = $.NSScreen.mainScreen.frame;
    screenW = frame.size.width;
    screenH = frame.size.height;
  } catch (eScreen) {}

  var elements = [];
  var count = 0;

  function emit(el) {
    var role = str(attr(el, "role", "AXRole"));
    var title = str(attr(el, "title", "AXTitle"));
    var desc = str(attr(el, "description", "AXDescription"));
    var value = str(attr(el, "value", "AXValue"));
    var enabledAttr = attr(el, "enabled", "AXEnabled");
    var x = 0, y = 0, w = 0, h = 0;
    try {
      var pos = el.position();
      if (pos) { x = pos[0]; y = pos[1]; }
    } catch (ePos) {}
    try {
      var size = el.size();
      if (size) { w = size[0]; h = size[1]; }
    } catch (eSize) {}
    elements.push({
      role: role, title: title, desc: desc, value: value,
      x: x, y: y, w: w, h: h,
      enabled: enabledAttr === true
    });
    count = count + 1;
  }

  function walk(el, depth) {
    if (count >= maxElements) { return; }
    if (depth > maxDepth) { return; }
    emit(el);
    if (count >= maxElements) { return; }
    var children = [];
    try { children = el.uiElements(); } catch (eChildren) { children = []; }
    var n = children.length;
    for (var i = 0; i < n; i++) {
      if (count >= maxElements) { break; }
      walk(children[i], depth + 1);
    }
  }

  var se = Application("System Events");
  var root = se.applicationProcesses.whose({frontmost: true})[0];
  if (root) {
    walk(root, 0);
  }

  return JSON.stringify({screen: {w: screenW, h: screenH}, elements: elements});
}
`

// jxaScript is jxaScriptTemplate with maxDepth/maxElements substituted in, so
// the walk's actual bounds and this doc comment's bounds can never drift
// apart.
var jxaScript = fmt.Sprintf(jxaScriptTemplate, maxDepth, maxElements)
