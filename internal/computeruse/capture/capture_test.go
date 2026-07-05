package capture_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gnanam1990/argus/internal/computeruse/approval"
	"github.com/gnanam1990/argus/internal/computeruse/capture"
	"github.com/gnanam1990/argus/internal/computeruse/instructions"
	"github.com/gnanam1990/argus/internal/computeruse/permissions"
	"github.com/gnanam1990/argus/internal/computeruse/state"
	"github.com/gnanam1990/argus/pkg/action"
)

// fakeOrch is a hermetic permissions.Orchestrator: Ensure returns the next
// error off a fixed queue (repeating the last entry once exhausted), never
// touching the OS.
type fakeOrch struct {
	errs  []error
	calls int
}

func (f *fakeOrch) Ensure(context.Context) error {
	idx := f.calls
	if idx >= len(f.errs) {
		idx = len(f.errs) - 1
	}
	f.calls++
	if idx < 0 {
		return nil
	}
	return f.errs[idx]
}

func (f *fakeOrch) IsLocked(context.Context) (bool, error) { return false, nil }

// fakeStore is a hermetic approval.Store backed by an in-memory decision.
type fakeStore struct {
	decision approval.Decision
	err      error
}

func (f *fakeStore) Get(context.Context, string) (approval.Decision, error) {
	return f.decision, f.err
}
func (f *fakeStore) Set(context.Context, string, approval.Decision) error { return nil }
func (f *fakeStore) Remove(context.Context, string) error                 { return nil }
func (f *fakeStore) List(context.Context) ([]approval.Record, error)      { return nil, nil }

// fakeFocuser is a hermetic Focuser recording the bundle it was asked to
// activate.
type fakeFocuser struct {
	err    error
	called bool
	got    string
}

func (f *fakeFocuser) Focus(_ context.Context, bundleID string) error {
	f.called = true
	f.got = bundleID
	return f.err
}

// fakeShot is a hermetic Screenshotter.
type fakeShot struct {
	img action.Image
	err error
}

func (f *fakeShot) Screenshot(context.Context) (action.Image, error) { return f.img, f.err }

// fakeGrounding is a hermetic grounding.Provider.
type fakeGrounding struct {
	root  state.Element
	err   error
	calls int
}

func (f *fakeGrounding) FrontmostTree(context.Context, string) (state.Element, error) {
	f.calls++
	return f.root, f.err
}

// fakeLoader is a hermetic instructions.Loader.
type fakeLoader struct {
	inst instructions.Instruction
	err  error
}

func (f *fakeLoader) Load(context.Context, string) (instructions.Instruction, error) {
	return f.inst, f.err
}

// virtualClock lets tests control the passage of time deterministically:
// now() reads it, and the sleep function injected via WithSleep advances it
// by the requested duration instead of actually blocking.
type virtualClock struct {
	t time.Time
}

func (c *virtualClock) now() time.Time { return c.t }

func (c *virtualClock) sleep(_ context.Context, d time.Duration) error {
	c.t = c.t.Add(d)
	return nil
}

// deps bundles a full set of working fakes for the happy path; each test
// mutates the piece it wants to exercise.
type deps struct {
	orch    *fakeOrch
	store   *fakeStore
	focuser *fakeFocuser
	shot    *fakeShot
	ground  *fakeGrounding
	loader  *fakeLoader
}

func happyDeps() *deps {
	return &deps{
		orch:    &fakeOrch{errs: []error{nil}},
		store:   &fakeStore{decision: approval.Approved},
		focuser: &fakeFocuser{},
		shot:    &fakeShot{img: action.Image{Data: []byte("png-bytes"), MIME: "image/png"}},
		ground: &fakeGrounding{root: state.Element{
			Index: 0,
			Role:  "AXWindow",
			Label: "My Window",
			Frame: state.Rect{X: 1, Y: 2, Width: 300, Height: 400},
			Children: []state.Element{
				{Index: 1, Role: "AXButton", Label: "OK"},
			},
		}},
		loader: &fakeLoader{inst: instructions.Instruction{
			BundleIdentifier: "com.example.app",
			AppName:          "Example",
			Markdown:         "# hi",
		}},
	}
}

func newWorker(d *deps, opts ...capture.Option) *capture.DefaultWorker {
	// Default the frontmost check to "unverifiable" so existing tests (which
	// don't control the real frontmost app) exercise the rest of the pipeline;
	// a test that wants the check active overrides this via a later option.
	all := append([]capture.Option{capture.WithFrontmostFunc(func() string { return "" })}, opts...)
	return capture.NewDefaultWorker(d.orch, d.store, d.focuser, d.ground, d.loader, d.shot, all...)
}

// drain collects every Update from ch until it closes.
func drain(t *testing.T, ch <-chan capture.Update) []capture.Update {
	t.Helper()
	var out []capture.Update
	timeout := time.After(5 * time.Second)
	for {
		select {
		case u, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, u)
		case <-timeout:
			t.Fatal("timed out waiting for capture worker to close its channel")
		}
	}
}

func TestStart_RejectsEmptyBundleIdentifier(t *testing.T) {
	t.Parallel()
	w := newWorker(happyDeps())
	_, err := w.Start(context.Background(), capture.Request{})
	if err == nil {
		t.Fatal("Start() with empty BundleIdentifier should error")
	}
}

func TestHappyPath_AssemblesAppState(t *testing.T) {
	t.Parallel()
	d := happyDeps()
	w := newWorker(d)

	ch, err := w.Start(context.Background(), capture.Request{RequestID: "r1", BundleIdentifier: "com.example.app"})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	updates := drain(t, ch)
	if len(updates) != 1 {
		t.Fatalf("updates = %+v, want exactly 1", updates)
	}
	got := updates[0]
	if got.Type != capture.UpdateCompleted {
		t.Fatalf("Type = %v, want UpdateCompleted (error: %s)", got.Type, got.Error)
	}
	if got.RequestID != "r1" {
		t.Errorf("RequestID = %q, want r1", got.RequestID)
	}
	st := got.State
	if st.BundleIdentifier != "com.example.app" {
		t.Errorf("BundleIdentifier = %q", st.BundleIdentifier)
	}
	if st.WindowTitle != "My Window" {
		t.Errorf("WindowTitle = %q, want My Window", st.WindowTitle)
	}
	if st.WindowFrame != (state.Rect{X: 1, Y: 2, Width: 300, Height: 400}) {
		t.Errorf("WindowFrame = %+v", st.WindowFrame)
	}
	if len(st.Elements) != 1 || st.Elements[0].Label != "OK" {
		t.Errorf("Elements = %+v, want root's single child [OK]", st.Elements)
	}
	if string(st.Screenshot.Data) != "png-bytes" {
		t.Errorf("Screenshot = %+v", st.Screenshot)
	}
	if st.Instruction != "# hi" {
		t.Errorf("Instruction = %q, want '# hi'", st.Instruction)
	}
	if !d.focuser.called || d.focuser.got != "com.example.app" {
		t.Errorf("focuser not called with the right bundle: %+v", d.focuser)
	}
}

func TestHappyPath_NoChildrenFallsBackToRootElement(t *testing.T) {
	t.Parallel()
	d := happyDeps()
	d.ground = &fakeGrounding{root: state.Element{Index: 0, Role: "AXWindow", Label: "Solo"}}
	w := newWorker(d)

	ch, err := w.Start(context.Background(), capture.Request{BundleIdentifier: "com.example.app"})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	updates := drain(t, ch)
	if len(updates) != 1 || updates[0].Type != capture.UpdateCompleted {
		t.Fatalf("updates = %+v", updates)
	}
	els := updates[0].State.Elements
	if len(els) != 1 || els[0].Role != "AXWindow" || els[0].Label != "Solo" {
		t.Errorf("Elements = %+v, want [root] fallback", els)
	}
}

func TestUnapprovedApp_FailsWithExactMessage(t *testing.T) {
	t.Parallel()
	for _, decision := range []approval.Decision{approval.Denied, approval.Pending} {
		decision := decision
		t.Run(string(decision), func(t *testing.T) {
			t.Parallel()
			d := happyDeps()
			d.store = &fakeStore{decision: decision}
			w := newWorker(d)

			ch, err := w.Start(context.Background(), capture.Request{BundleIdentifier: "com.example.app"})
			if err != nil {
				t.Fatalf("Start() error = %v", err)
			}
			updates := drain(t, ch)
			if len(updates) != 1 || updates[0].Type != capture.UpdateFailed {
				t.Fatalf("updates = %+v, want a single UpdateFailed", updates)
			}
			want := "Computer Use is not allowed to use the app 'com.example.app'. Ask the user for approval."
			if updates[0].Error != want {
				t.Errorf("Error = %q, want %q", updates[0].Error, want)
			}
			if d.focuser.called {
				t.Error("focuser must not be called for an unapproved app")
			}
		})
	}
}

func TestScreenLockedOrMissingPermissions_FailsImmediately(t *testing.T) {
	t.Parallel()
	for _, sentinel := range []error{permissions.ErrScreenLocked, permissions.ErrPermissionsMissing, errors.New("boom")} {
		sentinel := sentinel
		t.Run(sentinel.Error(), func(t *testing.T) {
			t.Parallel()
			d := happyDeps()
			d.orch = &fakeOrch{errs: []error{sentinel}}
			w := newWorker(d)

			ch, err := w.Start(context.Background(), capture.Request{BundleIdentifier: "com.example.app"})
			if err != nil {
				t.Fatalf("Start() error = %v", err)
			}
			updates := drain(t, ch)
			if len(updates) != 1 || updates[0].Type != capture.UpdateFailed {
				t.Fatalf("updates = %+v, want a single UpdateFailed", updates)
			}
			if !strings.Contains(updates[0].Error, sentinel.Error()) {
				t.Errorf("Error = %q, want it to mention %q", updates[0].Error, sentinel.Error())
			}
			if d.focuser.called {
				t.Error("focuser must not be called when preconditions fail")
			}
		})
	}
}

func TestPending_RetriesThenSucceeds(t *testing.T) {
	t.Parallel()
	d := happyDeps()
	// ErrPending twice, then nil.
	d.orch = &fakeOrch{errs: []error{permissions.ErrPending, permissions.ErrPending, nil}}

	clock := &virtualClock{t: time.Unix(0, 0)}
	w := newWorker(d,
		capture.WithClock(clock.now),
		capture.WithSleep(clock.sleep),
		capture.WithTimeout(time.Minute),
		capture.WithRetryInterval(time.Second))

	ch, err := w.Start(context.Background(), capture.Request{BundleIdentifier: "com.example.app"})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	updates := drain(t, ch)
	if len(updates) != 3 {
		t.Fatalf("updates = %+v, want 2 Pending + 1 Completed", updates)
	}
	for i := 0; i < 2; i++ {
		if updates[i].Type != capture.UpdatePending {
			t.Errorf("update %d = %v, want UpdatePending", i, updates[i].Type)
		}
	}
	if updates[2].Type != capture.UpdateCompleted {
		t.Errorf("final update = %v, want UpdateCompleted (error: %s)", updates[2].Type, updates[2].Error)
	}
}

func TestPending_TimesOut(t *testing.T) {
	t.Parallel()
	d := happyDeps()
	d.orch = &fakeOrch{errs: []error{permissions.ErrPending}} // always pending

	clock := &virtualClock{t: time.Unix(0, 0)}
	w := newWorker(d,
		capture.WithClock(clock.now),
		capture.WithSleep(clock.sleep),
		capture.WithTimeout(5*time.Second),
		capture.WithRetryInterval(2*time.Second))

	ch, err := w.Start(context.Background(), capture.Request{BundleIdentifier: "com.example.app"})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	updates := drain(t, ch)
	if len(updates) == 0 {
		t.Fatal("expected at least one update")
	}
	last := updates[len(updates)-1]
	if last.Type != capture.UpdateFailed {
		t.Fatalf("last update = %v, want UpdateFailed after timeout", last.Type)
	}
	for _, u := range updates[:len(updates)-1] {
		if u.Type != capture.UpdatePending {
			t.Errorf("non-final update = %v, want UpdatePending", u.Type)
		}
	}
	if d.focuser.called {
		t.Error("focuser must not be called when preconditions never clear")
	}
}

func TestFocusError_Fails(t *testing.T) {
	t.Parallel()
	d := happyDeps()
	d.focuser = &fakeFocuser{err: errors.New("activation refused")}
	w := newWorker(d)

	ch, err := w.Start(context.Background(), capture.Request{BundleIdentifier: "com.example.app"})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	updates := drain(t, ch)
	if len(updates) != 1 || updates[0].Type != capture.UpdateFailed {
		t.Fatalf("updates = %+v, want a single UpdateFailed", updates)
	}
	if !strings.Contains(updates[0].Error, "activation refused") {
		t.Errorf("Error = %q, want it to mention the focus failure", updates[0].Error)
	}
}

func TestWrongAppFrontmost_FailsClosed(t *testing.T) {
	t.Parallel()
	d := happyDeps()
	// The requested app never becomes frontmost — a different app is in front.
	w := newWorker(d, capture.WithFrontmostFunc(func() string { return "com.other.app" }),
		capture.WithSleep(func(context.Context, time.Duration) error { return nil }))

	ch, err := w.Start(context.Background(), capture.Request{BundleIdentifier: "com.example.app"})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	updates := drain(t, ch)
	if len(updates) != 1 || updates[0].Type != capture.UpdateFailed {
		t.Fatalf("updates = %+v, want a single UpdateFailed", updates)
	}
	if !strings.Contains(updates[0].Error, "frontmost") || !strings.Contains(updates[0].Error, "com.other.app") {
		t.Errorf("Error = %q, want it to name the wrong frontmost app", updates[0].Error)
	}
	// It must fail closed BEFORE grounding the wrong app's tree.
	if d.ground.calls != 0 {
		t.Errorf("grounding was called %d times; must not ground the wrong app", d.ground.calls)
	}
}

func TestCorrectAppFrontmost_Proceeds(t *testing.T) {
	t.Parallel()
	d := happyDeps()
	w := newWorker(d, capture.WithFrontmostFunc(func() string { return "com.example.app" }))

	ch, err := w.Start(context.Background(), capture.Request{BundleIdentifier: "com.example.app"})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	updates := drain(t, ch)
	if len(updates) == 0 || updates[len(updates)-1].Type != capture.UpdateCompleted {
		t.Fatalf("updates = %+v, want a terminal UpdateCompleted", updates)
	}
}

func TestScreenshotError_Fails(t *testing.T) {
	t.Parallel()
	d := happyDeps()
	d.shot = &fakeShot{err: errors.New("capture denied")}
	w := newWorker(d)

	ch, err := w.Start(context.Background(), capture.Request{BundleIdentifier: "com.example.app"})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	updates := drain(t, ch)
	if len(updates) != 1 || updates[0].Type != capture.UpdateFailed {
		t.Fatalf("updates = %+v, want a single UpdateFailed", updates)
	}
	if !strings.Contains(updates[0].Error, "capture denied") {
		t.Errorf("Error = %q, want it to mention the screenshot failure", updates[0].Error)
	}
}

func TestGroundingError_Fails(t *testing.T) {
	t.Parallel()
	d := happyDeps()
	d.ground = &fakeGrounding{err: errors.New("tree unavailable")}
	w := newWorker(d)

	ch, err := w.Start(context.Background(), capture.Request{BundleIdentifier: "com.example.app"})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	updates := drain(t, ch)
	if len(updates) != 1 || updates[0].Type != capture.UpdateFailed {
		t.Fatalf("updates = %+v, want a single UpdateFailed", updates)
	}
	if !strings.Contains(updates[0].Error, "tree unavailable") {
		t.Errorf("Error = %q, want it to mention the grounding failure", updates[0].Error)
	}
}

func TestLoaderError_Fails(t *testing.T) {
	t.Parallel()
	d := happyDeps()
	d.loader = &fakeLoader{err: errors.New("instructions corrupt")}
	w := newWorker(d)

	ch, err := w.Start(context.Background(), capture.Request{BundleIdentifier: "com.example.app"})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	updates := drain(t, ch)
	if len(updates) != 1 || updates[0].Type != capture.UpdateFailed {
		t.Fatalf("updates = %+v, want a single UpdateFailed", updates)
	}
	if !strings.Contains(updates[0].Error, "instructions corrupt") {
		t.Errorf("Error = %q, want it to mention the loader failure", updates[0].Error)
	}
}

func TestCanceledContext_FailsWithoutFocusing(t *testing.T) {
	t.Parallel()
	d := happyDeps()
	w := newWorker(d)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ch, err := w.Start(ctx, capture.Request{BundleIdentifier: "com.example.app"})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	updates := drain(t, ch)
	if len(updates) != 1 || updates[0].Type != capture.UpdateFailed {
		t.Fatalf("updates = %+v, want a single UpdateFailed", updates)
	}
	if d.focuser.called {
		t.Error("focuser must not be called once ctx is already canceled")
	}
}

// --- Provider ---

// fakeWorker is a hermetic Worker whose Start replays a fixed sequence of
// Updates over a closed channel.
type fakeWorker struct {
	updates  []capture.Update
	startErr error
	gotReq   capture.Request
}

func (f *fakeWorker) Start(_ context.Context, req capture.Request) (<-chan capture.Update, error) {
	f.gotReq = req
	if f.startErr != nil {
		return nil, f.startErr
	}
	ch := make(chan capture.Update, len(f.updates))
	for _, u := range f.updates {
		ch <- u
	}
	close(ch)
	return ch, nil
}

// fakeLister is a hermetic AppLister.
type fakeLister struct {
	apps []state.AppInfo
	err  error
}

func (f *fakeLister) ListApps(context.Context) ([]state.AppInfo, error) { return f.apps, f.err }

func TestProvider_GetAppState_Completed(t *testing.T) {
	t.Parallel()
	want := state.AppState{BundleIdentifier: "com.example.app", WindowTitle: "Win"}
	w := &fakeWorker{updates: []capture.Update{
		{Type: capture.UpdatePending},
		{Type: capture.UpdateCompleted, State: want},
	}}
	p := capture.NewProvider(w, &fakeLister{})

	got, err := p.GetAppState(context.Background(), "com.example.app")
	if err != nil {
		t.Fatalf("GetAppState() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GetAppState() = %+v, want %+v", got, want)
	}
	if w.gotReq.BundleIdentifier != "com.example.app" {
		t.Errorf("Start() bundle = %q", w.gotReq.BundleIdentifier)
	}
}

func TestProvider_GetAppState_Failed(t *testing.T) {
	t.Parallel()
	w := &fakeWorker{updates: []capture.Update{
		{Type: capture.UpdateFailed, Error: "nope"},
	}}
	p := capture.NewProvider(w, &fakeLister{})

	_, err := p.GetAppState(context.Background(), "com.example.app")
	if err == nil || err.Error() != "nope" {
		t.Errorf("GetAppState() error = %v, want 'nope'", err)
	}
}

func TestProvider_GetAppState_StartError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("bad request")
	w := &fakeWorker{startErr: wantErr}
	p := capture.NewProvider(w, &fakeLister{})

	_, err := p.GetAppState(context.Background(), "com.example.app")
	if !errors.Is(err, wantErr) {
		t.Errorf("GetAppState() error = %v, want %v", err, wantErr)
	}
}

func TestProvider_GetAppState_ClosedWithoutTerminal(t *testing.T) {
	t.Parallel()
	w := &fakeWorker{updates: []capture.Update{{Type: capture.UpdatePending}}}
	p := capture.NewProvider(w, &fakeLister{})

	_, err := p.GetAppState(context.Background(), "com.example.app")
	if err == nil {
		t.Error("GetAppState() should error when the channel closes without a terminal update")
	}
}

func TestProvider_GetAppState_ContextCanceled(t *testing.T) {
	t.Parallel()
	// blockingWorker's channel is never written to or closed, so
	// GetAppState can only return via the ctx-cancellation branch.
	p := capture.NewProvider(&blockingWorker{}, &fakeLister{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.GetAppState(ctx, "com.example.app")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("GetAppState() error = %v, want context.Canceled", err)
	}
}

// blockingWorker is a hermetic Worker whose channel is never written to or
// closed.
type blockingWorker struct{}

func (blockingWorker) Start(context.Context, capture.Request) (<-chan capture.Update, error) {
	return make(chan capture.Update), nil
}

func TestProvider_ListApps_Delegates(t *testing.T) {
	t.Parallel()
	want := []state.AppInfo{{BundleIdentifier: "com.apple.finder", Name: "Finder"}}
	p := capture.NewProvider(&fakeWorker{}, &fakeLister{apps: want})

	got, err := p.ListApps(context.Background())
	if err != nil {
		t.Fatalf("ListApps() error = %v", err)
	}
	if len(got) != 1 || got[0].BundleIdentifier != "com.apple.finder" {
		t.Errorf("ListApps() = %+v, want %+v", got, want)
	}
}

func TestProvider_ListApps_PropagatesError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("enumeration failed")
	p := capture.NewProvider(&fakeWorker{}, &fakeLister{err: wantErr})

	_, err := p.ListApps(context.Background())
	if !errors.Is(err, wantErr) {
		t.Errorf("ListApps() error = %v, want %v", err, wantErr)
	}
}
