// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Bananenbaas/kb7-commander/screen/internal/runlock"
	"github.com/Bananenbaas/kb7-commander/screen/internal/supervisor"
	"github.com/Bananenbaas/kb7-commander/screen/internal/update"
)

// note is one balloon the updater asked for.
type note struct {
	title, text string
	action      bool
}

// fakeUI records what the updater shows and answers its questions.
type fakeUI struct {
	mu       sync.Mutex
	notes    []note
	asked    []string
	answer   bool
	logs     []string
	releases int
	download int
}

func (f *fakeUI) notified() []note {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]note(nil), f.notes...)
}

func (f *fakeUI) releaseCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.releases
}

func beta2() update.Release {
	return update.Release{
		Tag:  "v0.1.0-beta.2",
		Page: "https://github.com/Bananenbaas/kb7-commander/releases/tag/v0.1.0-beta.2",
		Assets: []update.Asset{
			{Name: update.ExeAsset, URL: "https://example.invalid/exe"},
			{Name: update.SumsAsset, URL: "https://example.invalid/sums"},
		},
	}
}

func beta1() update.Release {
	r := beta2()
	r.Tag = "v0.1.0-beta.1"
	return r
}

// newTestUpdater returns an updater for current whose release list is rels
// (or relErr) and whose download writes "new" to the destination.
func newTestUpdater(t *testing.T, current string, rels []update.Release, relErr error) (*updater, *fakeUI) {
	t.Helper()
	f := &fakeUI{answer: true}
	exe := filepath.Join(t.TempDir(), "commander.exe")
	write(t, exe, "old")
	u := &updater{
		current: current,
		exe:     exe,
		dir:     `D:\config`,
		releases: func(context.Context) ([]update.Release, error) {
			f.mu.Lock()
			f.releases++
			f.mu.Unlock()
			return rels, relErr
		},
		download: func(_ context.Context, _ update.Release, dst string) error {
			f.mu.Lock()
			f.download++
			f.mu.Unlock()
			return os.WriteFile(dst, []byte("new"), 0o644)
		},
		confirm: func(text string) bool {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.asked = append(f.asked, text)
			return f.answer
		},
		notify: func(title, text string, onClick func()) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.notes = append(f.notes, note{title, text, onClick != nil})
		},
		logf: func(format string, args ...any) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.logs = append(f.logs, fmt.Sprintf(format, args...))
		},
		start: func(string, []string) error { return errors.New("no starter in this test") },
	}
	return u, f
}

func TestManualCheckDev(t *testing.T) {
	u, f := newTestUpdater(t, "dev", []update.Release{beta2()}, nil)
	u.check(context.Background(), false)
	if n := f.notified(); len(n) != 0 {
		t.Errorf("automatic check of a dev build notified %v", n)
	}
	u.check(context.Background(), true)
	n := f.notified()
	if len(n) != 1 || !strings.Contains(n[0].title+n[0].text, "development build") {
		t.Errorf("manual check of a dev build notified %v, want one development build note", n)
	}
	if f.releaseCalls() != 0 {
		t.Errorf("releases called %d times for a dev build", f.releaseCalls())
	}
	if _, ok := u.offered(); ok {
		t.Error("a dev build offers an update")
	}
}

func TestCheckUpToDate(t *testing.T) {
	older := beta1()
	older.Tag = "v0.0.9"
	u, f := newTestUpdater(t, "v0.1.0-beta.1", []update.Release{older, beta1()}, nil)
	u.check(context.Background(), false)
	if n := f.notified(); len(n) != 0 {
		t.Errorf("automatic check notified %v", n)
	}
	u.check(context.Background(), true)
	n := f.notified()
	if len(n) != 1 || !strings.Contains(n[0].text, "up to date") || !strings.Contains(n[0].text, "v0.1.0-beta.1") {
		t.Errorf("manual check notified %v, want one up-to-date note naming the version", n)
	}
	if _, ok := u.offered(); ok {
		t.Error("an update is offered")
	}
}

func TestCheckAheadOfReleases(t *testing.T) {
	u, f := newTestUpdater(t, "v9.0.0", []update.Release{beta2()}, nil)
	u.check(context.Background(), true)
	if n := f.notified(); len(n) != 1 || !strings.Contains(n[0].text, "up to date") {
		t.Errorf("manual check ahead of every release notified %v", n)
	}
}

func TestCheckFails(t *testing.T) {
	u, f := newTestUpdater(t, "v0.1.0-beta.1", nil, errors.New("server answered 403 Forbidden"))
	u.check(context.Background(), false)
	if n := f.notified(); len(n) != 0 {
		t.Errorf("failed automatic check notified %v", n)
	}
	if !strings.Contains(strings.Join(f.logs, "\n"), "403") {
		t.Errorf("failed automatic check not logged: %q", f.logs)
	}
	u.check(context.Background(), true)
	n := f.notified()
	if len(n) != 1 || !strings.Contains(n[0].text, "403") {
		t.Errorf("failed manual check notified %v, want the error", n)
	}
}

func TestOfferOncePerVersion(t *testing.T) {
	u, f := newTestUpdater(t, "v0.1.0-beta.1", []update.Release{beta1(), beta2()}, nil)
	u.check(context.Background(), false)
	u.check(context.Background(), false)
	n := f.notified()
	if len(n) != 1 || !strings.Contains(n[0].title+n[0].text, "v0.1.0-beta.2") || !n[0].action {
		t.Fatalf("two automatic checks notified %v, want one clickable note for v0.1.0-beta.2", n)
	}
	rel, ok := u.offered()
	if !ok || rel.Tag != "v0.1.0-beta.2" {
		t.Errorf("offered() = %v, %v", rel.Tag, ok)
	}
	u.check(context.Background(), true)
	if n := f.notified(); len(n) != 2 || !n[1].action {
		t.Errorf("manual check after the offer notified %v, want a second clickable note", n)
	}
}

func TestBusy(t *testing.T) {
	u, f := newTestUpdater(t, "v0.1.0-beta.1", []update.Release{beta1()}, nil)
	entered := make(chan struct{})
	unblock := make(chan struct{})
	u.releases = func(context.Context) ([]update.Release, error) {
		close(entered)
		<-unblock
		return []update.Release{beta1()}, nil
	}
	done := make(chan struct{})
	go func() {
		u.check(context.Background(), false)
		close(done)
	}()
	<-entered
	u.check(context.Background(), false)
	if n := f.notified(); len(n) != 0 {
		t.Errorf("automatic check while busy notified %v", n)
	}
	u.check(context.Background(), true)
	n := f.notified()
	if len(n) != 1 || !strings.Contains(n[0].text, "already running") {
		t.Errorf("manual check while busy notified %v, want already running", n)
	}
	if _, err := u.install(context.Background(), beta2()); err == nil {
		t.Error("install while busy did not fail")
	}
	close(unblock)
	<-done
	// Free again: a manual check answers normally.
	u.releases = func(context.Context) ([]update.Release, error) { return []update.Release{beta1()}, nil }
	u.check(context.Background(), true)
	if n := f.notified(); !strings.Contains(n[len(n)-1].text, "up to date") {
		t.Errorf("check after the busy one notified %v", n)
	}
}

func TestInstallDeclined(t *testing.T) {
	u, f := newTestUpdater(t, "v0.1.0-beta.1", []update.Release{beta2()}, nil)
	f.answer = false
	u.check(context.Background(), false)
	restart, err := u.install(context.Background(), beta2())
	if err != nil || restart != nil {
		t.Errorf("declined install = %v, %v", restart != nil, err)
	}
	if len(f.asked) != 1 {
		t.Fatalf("asked %d times", len(f.asked))
	}
	for _, want := range []string{"v0.1.0-beta.1", "v0.1.0-beta.2", beta2().Page} {
		if !strings.Contains(f.asked[0], want) {
			t.Errorf("confirm text %q lacks %q", f.asked[0], want)
		}
	}
	if f.download != 0 {
		t.Error("declined install downloaded")
	}
	if rel, ok := u.offered(); !ok || rel.Tag != "v0.1.0-beta.2" {
		t.Error("declining dropped the offer")
	}
	if read(t, u.exe) != "old" || exists(u.exe+".new") || exists(u.exe+".old") {
		t.Error("declined install touched the files")
	}
}

func TestInstallNotWritable(t *testing.T) {
	u, f := newTestUpdater(t, "v0.1.0-beta.1", nil, nil)
	u.exe = filepath.Join(t.TempDir(), "missing", "commander.exe")
	restart, err := u.install(context.Background(), beta2())
	if err == nil || restart != nil {
		t.Fatalf("install in an unwritable folder = %v, %v", restart != nil, err)
	}
	if f.download != 0 {
		t.Error("download called")
	}
	if exists(u.exe + ".new") {
		t.Error(".new exists")
	}
	n := f.notified()
	if len(n) != 1 || !strings.Contains(n[0].text, "Move Commander") {
		t.Errorf("notified %v, want a note that says to move Commander", n)
	}
}

func TestInstallDownloadFails(t *testing.T) {
	u, f := newTestUpdater(t, "v0.1.0-beta.1", nil, nil)
	u.download = func(_ context.Context, _ update.Release, dst string) error {
		// As update.Client.Download does: dst is removed on failure.
		os.WriteFile(dst, []byte("half"), 0o644)
		os.Remove(dst)
		return errors.New("checksum of commander-windows-amd64.exe does not match")
	}
	restart, err := u.install(context.Background(), beta2())
	if err == nil || restart != nil {
		t.Fatalf("install with a failing download = %v, %v", restart != nil, err)
	}
	if exists(u.exe+".new") || exists(u.exe+".old") || read(t, u.exe) != "old" {
		t.Error("a failed download changed the files")
	}
	n := f.notified()
	if len(n) != 1 || !strings.Contains(n[0].text, "checksum") {
		t.Errorf("notified %v, want the download error", n)
	}
	if _, err := u.install(context.Background(), beta2()); err == nil {
		t.Error("second attempt did not fail the same way")
	} else if strings.Contains(err.Error(), "already running") {
		t.Errorf("a failed install kept the updater busy: %v", err)
	}
}

func TestInstallCancelledAfterDownload(t *testing.T) {
	u, _ := newTestUpdater(t, "v0.1.0-beta.1", nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	u.download = func(_ context.Context, _ update.Release, dst string) error {
		cancel() // Quit chosen just as the download finishes
		return os.WriteFile(dst, []byte("new"), 0o644)
	}
	restart, err := u.install(ctx, beta2())
	if err == nil || restart != nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("install = %v, %v", restart != nil, err)
	}
	if read(t, u.exe) != "old" || exists(u.exe+".new") || exists(u.exe+".old") {
		t.Error("a cancelled install touched the files")
	}
	u.waitSwap()
	if !u.begin() {
		t.Error("a cancelled install kept the updater busy")
	}
}

func TestInstallLeftoverNew(t *testing.T) {
	u, _ := newTestUpdater(t, "v0.1.0-beta.1", nil, nil)
	write(t, u.exe+".new", "stale")
	restart, err := u.install(context.Background(), beta2())
	if err != nil || restart == nil {
		t.Fatalf("install over a stale .new = %v, %v", restart != nil, err)
	}
	if read(t, u.exe) != "new" {
		t.Errorf("exe holds %q", read(t, u.exe))
	}
}

func TestInstallSwaps(t *testing.T) {
	u, f := newTestUpdater(t, "v0.1.0-beta.1", []update.Release{beta2()}, nil)
	u.check(context.Background(), false)
	restart, err := u.install(context.Background(), beta2())
	if err != nil || restart == nil {
		t.Fatalf("install = %v, %v", restart != nil, err)
	}
	if read(t, u.exe) != "new" || read(t, u.exe+".old") != "old" || exists(u.exe+".new") {
		t.Fatalf("after install: exe %q, .old %q", read(t, u.exe), read(t, u.exe+".old"))
	}
	if _, ok := u.offered(); ok {
		t.Error("an installed update is still offered")
	}
	// The updater stays busy: Commander is about to end.
	u.check(context.Background(), true)
	if n := f.notified(); len(n) < 1 || !strings.Contains(n[len(n)-1].text, "already running") {
		t.Errorf("check after a finished install notified %v", n)
	}

	var calls [][]string
	u.start = func(exe string, args []string) error {
		if exe != u.exe {
			t.Errorf("started %q, want %q", exe, u.exe)
		}
		calls = append(calls, args)
		return errors.New("cannot start")
	}
	if err := restart(); err == nil {
		t.Error("restart with a failing starter did not fail")
	}
	want := [][]string{{`D:\config`, "--updated-from", "v0.1.0-beta.1"}, {`D:\config`}}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("starts %q, want %q", calls, want)
	}
	if read(t, u.exe) != "old" || exists(u.exe+".old") {
		t.Errorf("after the failed restart: exe %q, .old exists %v", read(t, u.exe), exists(u.exe+".old"))
	}
}

func TestRestartStartsNew(t *testing.T) {
	u, _ := newTestUpdater(t, "v0.1.0-beta.1", nil, nil)
	restart, err := u.install(context.Background(), beta2())
	if err != nil || restart == nil {
		t.Fatalf("install = %v, %v", restart != nil, err)
	}
	var calls [][]string
	u.start = func(_ string, args []string) error {
		calls = append(calls, args)
		return nil
	}
	if err := restart(); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{`D:\config`, "--updated-from", "v0.1.0-beta.1"}}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("starts %q, want %q", calls, want)
	}
	if read(t, u.exe) != "new" || read(t, u.exe+".old") != "old" {
		t.Error("a good restart undid the swap")
	}
}

func TestInstallOffer(t *testing.T) {
	u, _ := newTestUpdater(t, "v0.1.0-beta.1", []update.Release{beta2()}, nil)
	stopped := 0
	u.stop = func() { stopped++ }
	u.installOffer(context.Background())
	if stopped != 0 || u.pendingRestart() != nil {
		t.Fatal("installOffer without an offer did something")
	}
	u.check(context.Background(), false)
	u.installOffer(context.Background())
	if stopped != 1 || u.pendingRestart() == nil {
		t.Errorf("after installOffer: stopped %d, restart pending %v", stopped, u.pendingRestart() != nil)
	}
}

func TestUpdatedFromRetry(t *testing.T) {
	held := fmt.Errorf("already running: %w", runlock.ErrHeld)
	userHeld := fmt.Errorf("already running: %w", runlock.ErrUserHeld)
	other := errors.New("not a folder")
	sup := new(supervisor.Supervisor)

	// fails returns a newSup that fails with errs in turn, then succeeds.
	fails := func(calls *int, errs ...error) func() (*supervisor.Supervisor, error) {
		return func() (*supervisor.Supervisor, error) {
			*calls++
			if *calls <= len(errs) {
				return nil, errs[*calls-1]
			}
			return sup, nil
		}
	}
	ctx := context.Background()

	calls := 0
	if got, err := newWithRetry(ctx, fails(&calls), time.Second, true); got != sup || err != nil || calls != 1 {
		t.Errorf("success: %v, %v after %d calls", got, err, calls)
	}

	calls = 0
	if got, err := newWithRetry(ctx, fails(&calls, held, userHeld), 10*time.Second, true); got != sup || err != nil || calls != 3 {
		t.Errorf("held then free: %v, %v after %d calls", got, err, calls)
	}

	calls = 0
	begin := time.Now()
	always := func() (*supervisor.Supervisor, error) { calls++; return nil, held }
	if _, err := newWithRetry(ctx, always, 600*time.Millisecond, true); !errors.Is(err, runlock.ErrHeld) || calls < 2 {
		t.Errorf("always held: %v after %d calls", err, calls)
	}
	if d := time.Since(begin); d < 600*time.Millisecond || d > 3*time.Second {
		t.Errorf("always held gave up after %v", d)
	}

	calls = 0
	if _, err := newWithRetry(ctx, fails(&calls, other), 10*time.Second, true); err != other || calls != 1 {
		t.Errorf("other error: %v after %d calls", err, calls)
	}

	calls = 0
	if _, err := newWithRetry(ctx, fails(&calls, held), 10*time.Second, false); !errors.Is(err, runlock.ErrHeld) || calls != 1 {
		t.Errorf("without retry: %v after %d calls", err, calls)
	}
}
