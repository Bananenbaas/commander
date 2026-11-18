// SPDX-License-Identifier: GPL-2.0-or-later

package supervisor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Bananenbaas/kb7-commander/screen/internal/apply"
	"github.com/Bananenbaas/kb7-commander/screen/internal/layout"
	"github.com/Bananenbaas/kb7-commander/screen/internal/ledger"
	"github.com/Bananenbaas/kb7-commander/screen/internal/request"
	"github.com/Bananenbaas/kb7-commander/screen/internal/run"
	"github.com/Bananenbaas/kb7-commander/screen/internal/runlock"
)

const testPoll = 50 * time.Millisecond

// userLock stands in for the per-user lock, so the tests neither depend on
// nor disturb a loop that runs on this machine.
var userLock struct {
	mu   sync.Mutex
	held bool
}

func TestMain(m *testing.M) {
	acquireUser = func() (func() error, error) {
		userLock.mu.Lock()
		defer userLock.mu.Unlock()
		if userLock.held {
			return nil, runlock.ErrUserHeld
		}
		userLock.held = true
		var once sync.Once
		return func() error {
			once.Do(func() {
				userLock.mu.Lock()
				userLock.held = false
				userLock.mu.Unlock()
			})
			return nil
		}, nil
	}
	os.Exit(m.Run())
}

func userHeld() bool {
	userLock.mu.Lock()
	defer userLock.mu.Unlock()
	return userLock.held
}

// fake is a Load, Loop and Apply that record what they are asked.
type fake struct {
	mu       sync.Mutex
	loadErr  error
	loaded   []*layout.Config
	starts   []*layout.Config
	ctxs     []context.Context
	loopErr  error         // returned at once by the next loop started
	status   *run.Status   // reported by every loop started
	reports  []run.Status  // reported after status by every loop started
	endErr   error         // returned by the next loop started once it has reported
	resumes  []*run.Resume // the resume of every loop started
	applyErr error         // returned by Apply
	applyFor time.Duration // how long Apply takes
	applies  int
	applied  chan struct{} // closed when Apply has started
	finished time.Time     // when Apply returned

	states   []State
	inState  atomic.Int32
	overlaps atomic.Int32
}

func newFake() *fake { return &fake{applied: make(chan struct{})} }

func (f *fake) load(dir string) (*layout.Config, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	cfg := &layout.Config{Dir: dir}
	f.loaded = append(f.loaded, cfg)
	return cfg, nil
}

func (f *fake) loop(ctx context.Context, cfg *layout.Config, resume *run.Resume, status func(run.Status)) error {
	f.mu.Lock()
	f.starts = append(f.starts, cfg)
	f.ctxs = append(f.ctxs, ctx)
	f.resumes = append(f.resumes, resume)
	err, endErr := f.loopErr, f.endErr
	f.loopErr, f.endErr = nil, nil
	st, reports := f.status, f.reports
	f.mu.Unlock()
	if err != nil {
		return err
	}
	if st != nil {
		status(*st)
	}
	for _, r := range reports {
		status(r)
	}
	if endErr != nil {
		return endErr
	}
	<-ctx.Done()
	return nil
}

// resume returns the resume of the i-th loop started.
func (f *fake) resume(i int) *run.Resume {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.resumes[i]
}

func (f *fake) apply(ctx context.Context, cfg *layout.Config) (string, error) {
	f.mu.Lock()
	f.applies++
	if f.applies == 1 {
		close(f.applied)
	}
	d, err := f.applyFor, f.applyErr
	f.mu.Unlock()
	time.Sleep(d)
	f.mu.Lock()
	f.finished = time.Now()
	f.mu.Unlock()
	return "2 pictures uploaded, 3 unchanged", err
}

func (f *fake) onState(st State) {
	if f.inState.Add(1) > 1 {
		f.overlaps.Add(1)
	}
	f.mu.Lock()
	f.states = append(f.states, st)
	f.mu.Unlock()
	f.inState.Add(-1)
}

func (f *fake) set(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn()
}

func (f *fake) nStarts() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.starts)
}

func (f *fake) loopCtx(i int) context.Context {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ctxs[i]
}

func (f *fake) last() State {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.states) == 0 {
		return State{}
	}
	return f.states[len(f.states)-1]
}

func (f *fake) options() Options {
	return Options{
		Logf:    func(string, ...any) {},
		OnState: f.onState,
		Load:    f.load,
		Loop:    f.loop,
		Apply:   f.apply,
		Poll:    testPoll,
	}
}

// eventually waits up to 2 s for cond.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// serve starts Serve on its own goroutine and returns its result channel.
func serve(t *testing.T, s *Supervisor) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	ended := make(chan struct{})
	go func() {
		done <- s.Serve(context.Background())
		close(ended)
	}()
	t.Cleanup(func() {
		s.Stop()
		select {
		case <-ended:
		case <-time.After(5 * time.Second):
			t.Error("Serve did not return after Stop")
		}
	})
	return done
}

func waitServe(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve = %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return")
	}
}

func newSupervisor(t *testing.T, f *fake) (*Supervisor, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := New(dir, f.options())
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func TestStartsAndStopsCleanly(t *testing.T) {
	f := newFake()
	f.status = &run.Status{Profile: 2}
	s, dir := newSupervisor(t, f)
	done := serve(t, s)
	eventually(t, "the loop's status", func() bool {
		st := f.last()
		return st.Phase == Running && st.Run.Profile == 2
	})
	s.Stop()
	waitServe(t, done)
	if n := f.nStarts(); n != 1 {
		t.Fatalf("%d starts, want 1", n)
	}
	if f.loopCtx(0).Err() == nil {
		t.Fatal("the loop's context was not cancelled")
	}
	if st := f.last(); st.Phase != Stopped || st.Err != "" {
		t.Fatalf("last state %+v, want Stopped without error", st)
	}
	if held, err := runlock.Held(dir); err != nil || held {
		t.Fatalf("folder lock held after Serve: %v, %v", held, err)
	}
	if userHeld() {
		t.Fatal("user lock held after Serve")
	}
	if n := f.overlaps.Load(); n != 0 {
		t.Fatalf("OnState was called concurrently %d times", n)
	}
}

func TestServeEndsWithItsContext(t *testing.T) {
	f := newFake()
	s, _ := newSupervisor(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx) }()
	eventually(t, "a start", func() bool { return f.nStarts() == 1 })
	cancel()
	waitServe(t, done)
	if st := f.last(); st.Phase != Stopped {
		t.Fatalf("last state %+v, want Stopped", st)
	}
}

func TestBrokenConfigAtStartFails(t *testing.T) {
	f := newFake()
	f.loadErr = errors.New("a.toml: profile is required")
	s, _ := newSupervisor(t, f)
	serve(t, s)
	eventually(t, "Failed", func() bool { return f.last().Phase == Failed })
	if st := f.last(); !strings.Contains(st.Err, "a.toml: profile is required") {
		t.Fatalf("Err = %q, want the load error", st.Err)
	}
	if n := f.nStarts(); n != 0 {
		t.Fatalf("%d starts with a broken configuration", n)
	}
	f.set(func() { f.loadErr = nil })
	s.Reload()
	eventually(t, "Running", func() bool { return f.last().Phase == Running && f.nStarts() == 1 })
	if st := f.last(); st.Err != "" {
		t.Fatalf("Err = %q after a good reload", st.Err)
	}
}

func TestLoopErrorFails(t *testing.T) {
	f := newFake()
	f.loopErr = errors.New("run: profile 1 position 3: a hold button needs exactly one key chord")
	s, _ := newSupervisor(t, f)
	serve(t, s)
	eventually(t, "Failed", func() bool { return f.last().Phase == Failed })
	if st := f.last(); !strings.Contains(st.Err, "hold button") {
		t.Fatalf("Err = %q, want the loop's error", st.Err)
	}
	s.Reload()
	eventually(t, "Running", func() bool { return f.last().Phase == Running && f.nStarts() == 2 })
}

func TestReloadKeepsOldOnError(t *testing.T) {
	f := newFake()
	s, _ := newSupervisor(t, f)
	serve(t, s)
	eventually(t, "a start", func() bool { return f.nStarts() == 1 })
	f.set(func() { f.loadErr = errors.New("b.toml: duplicate button") })
	s.Reload()
	eventually(t, "the reload error", func() bool { return f.last().Err != "" })
	st := f.last()
	if st.Phase != Running || !strings.Contains(st.Err, "b.toml: duplicate button") {
		t.Fatalf("state %+v, want Running with the load error", st)
	}
	time.Sleep(2 * testPoll)
	if n := f.nStarts(); n != 1 {
		t.Fatalf("%d starts, want the old loop kept", n)
	}
	if f.loopCtx(0).Err() != nil {
		t.Fatal("the old loop was stopped")
	}
}

func TestReloadRestartsWithNewConfig(t *testing.T) {
	f := newFake()
	s, _ := newSupervisor(t, f)
	serve(t, s)
	eventually(t, "a start", func() bool { return f.nStarts() == 1 })
	s.Reload()
	eventually(t, "a second start", func() bool { return f.nStarts() == 2 })
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.loaded) != 2 || f.starts[1] != f.loaded[1] || f.starts[0] == f.starts[1] {
		t.Fatalf("second start with %p, want the newly loaded %p", f.starts[1], f.loaded[len(f.loaded)-1])
	}
	if f.ctxs[0].Err() == nil {
		t.Fatal("the first loop was not stopped")
	}
}

func TestApplyRestartsLoopEvenOnRefusal(t *testing.T) {
	f := newFake()
	f.applyErr = errors.New("SwarmII.exe is running; close it first")
	s, _ := newSupervisor(t, f)
	serve(t, s)
	eventually(t, "a start", func() bool { return f.nStarts() == 1 })
	s.Apply()
	eventually(t, "a second start", func() bool { return f.nStarts() == 2 })
	eventually(t, "Running", func() bool { return f.last().Phase == Running })
	if st := f.last(); !strings.Contains(st.Err, "SwarmII.exe is running") {
		t.Fatalf("Err = %q, want the refusal", st.Err)
	}
	if f.loopCtx(0).Err() == nil {
		t.Fatal("the loop was not stopped for Apply")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	sawApplying := false
	for _, st := range f.states {
		if st.Phase == Applying {
			sawApplying = true
		}
	}
	if !sawApplying {
		t.Fatal("no Applying state reported")
	}
}

func TestApplyReportsItsSummary(t *testing.T) {
	f := newFake()
	s, _ := newSupervisor(t, f)
	serve(t, s)
	eventually(t, "a start", func() bool { return f.nStarts() == 1 })
	s.Apply()
	eventually(t, "the summary", func() bool { return f.last().Note != "" })
	if st := f.last(); st.Phase != Running || st.Err != "" || st.Note != "2 pictures uploaded, 3 unchanged" {
		t.Fatalf("state %+v, want Running with the summary", st)
	}
}

func TestRequestsWaitForApply(t *testing.T) {
	f := newFake()
	f.applyFor = 300 * time.Millisecond
	s, _ := newSupervisor(t, f)
	done := serve(t, s)
	eventually(t, "a start", func() bool { return f.nStarts() == 1 })
	s.Apply()
	<-f.applied
	s.Stop()
	waitServe(t, done)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.finished.IsZero() {
		t.Fatal("Serve returned before Apply finished")
	}
	if st := f.states[len(f.states)-1]; st.Phase != Stopped {
		t.Fatalf("last state %+v, want Stopped", st)
	}
}

func TestStopRequestFileDuringApply(t *testing.T) {
	f := newFake()
	f.applyFor = 300 * time.Millisecond
	s, dir := newSupervisor(t, f)
	done := serve(t, s)
	eventually(t, "a start", func() bool { return f.nStarts() == 1 })
	s.Apply()
	<-f.applied
	if err := request.Write(dir, request.Stop); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the request taken", func() bool {
		_, err := os.Stat(filepath.Join(dir, request.FileName))
		return os.IsNotExist(err)
	})
	waitServe(t, done)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.finished.IsZero() {
		t.Fatal("Serve returned before Apply finished")
	}
	if len(f.starts) != 1 {
		t.Fatalf("%d starts, want no restart after the Apply", len(f.starts))
	}
	st := f.states[len(f.states)-1]
	if st.Phase != Stopped || st.Note != "2 pictures uploaded, 3 unchanged" {
		t.Fatalf("last state %+v, want Stopped with the Apply's summary", st)
	}
}

func TestReloadAfterApplyStillRestarts(t *testing.T) {
	f := newFake()
	f.applyFor = 100 * time.Millisecond
	s, _ := newSupervisor(t, f)
	serve(t, s)
	eventually(t, "a start", func() bool { return f.nStarts() == 1 })
	s.Apply()
	<-f.applied
	s.Reload()
	eventually(t, "a third start", func() bool { return f.nStarts() == 3 })
}

func TestCloseWithoutServe(t *testing.T) {
	f := newFake()
	s, dir := newSupervisor(t, f)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if held, err := runlock.Held(dir); err != nil || held {
		t.Fatalf("folder lock held after Close: %v, %v", held, err)
	}
	if userHeld() {
		t.Fatal("user lock held after Close")
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close = %v", err)
	}
	if err := s.Serve(context.Background()); err == nil {
		t.Fatal("Serve after Close succeeded")
	}
}

func TestCloseAfterServeDoesNothing(t *testing.T) {
	f := newFake()
	s, _ := newSupervisor(t, f)
	done := serve(t, s)
	eventually(t, "a start", func() bool { return f.nStarts() == 1 })
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if !userHeld() {
		t.Fatal("Close released the locks while Serve runs")
	}
	s.Stop()
	waitServe(t, done)
	if userHeld() {
		t.Fatal("user lock held after Serve")
	}
}

func TestPanicInServeReleasesTheLocks(t *testing.T) {
	f := newFake()
	o := f.options()
	var panicked atomic.Bool
	o.OnState = func(st State) {
		if st.Phase == Running && panicked.CompareAndSwap(false, true) {
			panic("boom")
		}
	}
	dir := t.TempDir()
	s, err := New(dir, o)
	if err != nil {
		t.Fatal(err)
	}
	f.set(func() { f.status = nil })
	func() {
		defer func() {
			if r := recover(); r != "boom" {
				t.Fatalf("recovered %v, want the panic", r)
			}
		}()
		s.Serve(context.Background())
	}()
	if held, err := runlock.Held(dir); err != nil || held {
		t.Fatalf("folder lock held after a panic: %v, %v", held, err)
	}
	if userHeld() {
		t.Fatal("user lock held after a panic")
	}
}

func TestStaleRequestIsCleared(t *testing.T) {
	f := newFake()
	dir := t.TempDir()
	if err := request.Write(dir, request.Stop); err != nil {
		t.Fatal(err)
	}
	s, err := New(dir, f.options())
	if err != nil {
		t.Fatal(err)
	}
	done := serve(t, s)
	eventually(t, "a start", func() bool { return f.nStarts() == 1 })
	select {
	case err := <-done:
		t.Fatalf("Serve returned %v: the stale request stopped it", err)
	case <-time.After(3 * testPoll):
	}
	if f.loopCtx(0).Err() != nil {
		t.Fatal("the loop was stopped by the stale request")
	}
}

func TestStopRequestFile(t *testing.T) {
	f := newFake()
	s, dir := newSupervisor(t, f)
	done := serve(t, s)
	eventually(t, "a start", func() bool { return f.nStarts() == 1 })
	if err := request.Write(dir, request.Stop); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve = %v", err)
		}
	case <-time.After(2*testPoll + 250*time.Millisecond):
		t.Fatal("Serve did not stop within two polls")
	}
	if _, err := os.Stat(dir + "/" + request.FileName); !os.IsNotExist(err) {
		t.Fatalf("the request file is still there: %v", err)
	}
}

func TestReloadRequestFile(t *testing.T) {
	f := newFake()
	s, dir := newSupervisor(t, f)
	serve(t, s)
	eventually(t, "a start", func() bool { return f.nStarts() == 1 })
	if err := request.Write(dir, request.Reload); err != nil {
		t.Fatal(err)
	}
	eventually(t, "a second start", func() bool { return f.nStarts() == 2 })
}

func TestSecondSupervisorRefused(t *testing.T) {
	f := newFake()
	s, dir := newSupervisor(t, f)
	serve(t, s)
	if _, err := New(t.TempDir(), newFake().options()); !errors.Is(err, runlock.ErrUserHeld) {
		t.Fatalf("second New on another folder = %v, want ErrUserHeld", err)
	}
	if _, err := New(dir, newFake().options()); !errors.Is(err, runlock.ErrHeld) {
		t.Fatalf("second New on the same folder = %v, want ErrHeld", err)
	}
}

func TestNewRefusesAMissingFolder(t *testing.T) {
	if _, err := New(t.TempDir()+"/nope", newFake().options()); err == nil {
		t.Fatal("New on a missing folder succeeded")
	}
	if userHeld() {
		t.Fatal("user lock kept after a failed New")
	}
}

func TestSummary(t *testing.T) {
	o := ledger.Owner{}
	for _, c := range []struct {
		rep  apply.Report
		want string
	}{
		{apply.Report{}, "0 pictures uploaded, 0 unchanged"},
		{apply.Report{Uploaded: []ledger.Owner{o}, Skipped: []ledger.Owner{o, o}}, "1 picture uploaded, 2 unchanged"},
		{apply.Report{Restored: []ledger.Owner{o}, Notes: []string{"a", "b"}}, "0 pictures uploaded, 0 unchanged, 1 position restored, 2 notes in the log"},
	} {
		if got := summary(c.rep); got != c.want {
			t.Errorf("summary = %q, want %q", got, c.want)
		}
	}
}

// wantResume checks the resume of the i-th loop started.
func wantResume(t *testing.T, f *fake, i int, want *run.Resume) {
	t.Helper()
	got := f.resume(i)
	if (got == nil) != (want == nil) || (got != nil && *got != *want) {
		t.Fatalf("loop %d resumed %+v, want %+v", i, got, want)
	}
}

func TestReloadResumesTheActiveProfile(t *testing.T) {
	f := newFake()
	f.status = &run.Status{Profile: 4, Name: "DEFAULT 03"}
	s, _ := newSupervisor(t, f)
	serve(t, s)
	eventually(t, "the loop's status", func() bool { return f.last().Run.Profile == 4 })
	s.Reload()
	eventually(t, "a second start", func() bool { return f.nStarts() == 2 })
	wantResume(t, f, 0, nil)
	wantResume(t, f, 1, &run.Resume{Profile: 4, Active: true})
}

// A loop that lost the keyboard tells nothing about the profile.
func TestReloadAfterADisconnectDoesNotResume(t *testing.T) {
	for _, waiting := range []string{"keyboard", "Swarm II", run.WaitReplug} {
		t.Run(waiting, func(t *testing.T) {
			f := newFake()
			f.status = &run.Status{Profile: 4}
			f.reports = []run.Status{{Profile: -1, Waiting: waiting}}
			s, _ := newSupervisor(t, f)
			serve(t, s)
			eventually(t, "the loop's status", func() bool { return f.last().Run.Waiting == waiting })
			s.Reload()
			eventually(t, "a second start", func() bool { return f.nStarts() == 2 })
			wantResume(t, f, 1, nil)
		})
	}
}

func TestReloadBeforeAnyStatusDoesNotResume(t *testing.T) {
	f := newFake()
	s, _ := newSupervisor(t, f)
	serve(t, s)
	eventually(t, "a start", func() bool { return f.nStarts() == 1 })
	s.Reload()
	eventually(t, "a second start", func() bool { return f.nStarts() == 2 })
	wantResume(t, f, 1, nil)
}

// Apply switches profiles: the profile is kept, but no longer known to be
// active.
func TestApplyResumesWithoutKnowingTheActiveProfile(t *testing.T) {
	f := newFake()
	f.status = &run.Status{Profile: 4}
	s, _ := newSupervisor(t, f)
	serve(t, s)
	eventually(t, "the loop's status", func() bool { return f.last().Run.Profile == 4 })
	s.Apply()
	eventually(t, "a second start", func() bool { return f.nStarts() == 2 })
	wantResume(t, f, 1, &run.Resume{Profile: 4})
}

// The resume is that of the loop just stopped, not of an older one.
func TestResumeFollowsTheLastLoop(t *testing.T) {
	f := newFake()
	f.status = &run.Status{Profile: 4}
	s, _ := newSupervisor(t, f)
	serve(t, s)
	eventually(t, "the loop's status", func() bool { return f.last().Run.Profile == 4 })
	f.set(func() { f.status = nil })
	s.Reload()
	eventually(t, "a second start", func() bool { return f.nStarts() == 2 })
	s.Reload()
	eventually(t, "a third start", func() bool { return f.nStarts() == 3 })
	wantResume(t, f, 2, nil)
}

// A loop that ended by itself leaves nothing to resume.
func TestNoResumeAfterTheLoopFailed(t *testing.T) {
	f := newFake()
	f.status = &run.Status{Profile: 4}
	f.endErr = errors.New("boom")
	s, _ := newSupervisor(t, f)
	serve(t, s)
	eventually(t, "Failed", func() bool { return f.last().Phase == Failed })
	s.Reload()
	eventually(t, "a second start", func() bool { return f.nStarts() == 2 })
	wantResume(t, f, 1, nil)
}

// A profile the keyboard did not confirm is chosen again, but not assumed.
func TestReloadAfterAnUnconfirmedSwitch(t *testing.T) {
	f := newFake()
	f.status = &run.Status{Profile: 4, Unconfirmed: true}
	s, _ := newSupervisor(t, f)
	serve(t, s)
	eventually(t, "the loop's status", func() bool { return f.last().Run.Profile == 4 })
	s.Reload()
	eventually(t, "a second start", func() bool { return f.nStarts() == 2 })
	wantResume(t, f, 1, &run.Resume{Profile: 4})
}
