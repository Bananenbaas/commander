// SPDX-License-Identifier: GPL-2.0-or-later

// Package apply puts the configured layouts on the keyboard and gives the
// positions kb7screen has taken over back their original contents.
//
// Nothing is retried: the first keyboard error stops the work and is returned
// with its context. The ledger is changed in memory; it is stored through the
// caller's save function at fixed points, and the caller saves it once more
// when Apply or Restore returns.
//
// Cancelling the context stops Apply and Restore only where nothing is left
// half done: never during a picture upload, and never between a profile's
// table writes. They then return ErrStopped.
package apply

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Bananenbaas/kb7-commander/screen/internal/device"
	"github.com/Bananenbaas/kb7-commander/screen/internal/layout"
	"github.com/Bananenbaas/kb7-commander/screen/internal/ledger"
	"github.com/Bananenbaas/kb7-commander/screen/internal/light"
	"github.com/Bananenbaas/kb7-commander/screen/internal/picture"
	"github.com/Bananenbaas/kb7-commander/screen/internal/proto"
)

// Keyboard holds the keyboard operations apply and restore use. The
// keyboard package's *KB provides them.
type Keyboard interface {
	EnableNotifications() error
	SwitchProfile(p byte) error
	SwitchProfileFresh(p byte) error
	NextEvent(timeout time.Duration) (any, error)
	ReadButtons(p byte) (*proto.Table29, error)
	ReadTypes(p byte) (*proto.Table25, error)
	Upload(slot byte, data []byte) error
	WriteButtons(t *proto.Table29) error
	WriteTypes(t *proto.Table25) error
	WaitRedraw(timeout time.Duration) error
	ReadLightsTable(p byte) (*proto.Table11, error)
	WriteLightsTable(t *proto.Table11) error
	ReadNamesTable() (*proto.Table16, error)
	WriteNamesTable(t *proto.Table16) error
}

// ErrStopped is returned, wrapping the context's error, when Apply or Restore
// stopped because its context was cancelled.
var ErrStopped = errors.New("apply: stopped")

// stopped returns ErrStopped once ctx is cancelled, and nil before.
func stopped(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w (%w)", ErrStopped, err)
	}
	return nil
}

// Rename is one profile given a new name.
type Rename struct {
	Profile byte
	Name    string
}

// Report says what Apply or Restore did.
type Report struct {
	Uploaded, Skipped []ledger.Owner // buttons whose picture was sent, or was already in its slot
	Restored          []ledger.Owner // positions given back their original contents
	Lighting          []byte         // profiles whose stored lighting was written
	Renamed           []Rename       // profiles given a new name
	Notes             []string       // things the user should know about
}

const (
	maxProfile = 4

	noProfile = -1               // the active profile is not known
	eventWait = time.Millisecond // longest wait for one report of waiting input
	maxEvents = 64               // most waiting reports read before a switch is left out

	redrawTimeout = 5 * time.Second // wait for the redraw that follows a type table
)

// session is the keyboard during one Apply or Restore. It remembers the
// profile the keyboard last confirmed, so that a switch to the profile that
// is already active is not sent: the keyboard does not confirm such a switch.
// While the active profile is not known, as at the first switch, the switch
// goes through another profile first (SwitchProfileFresh).
type session struct {
	kb     Keyboard
	active int // the profile the keyboard last confirmed, or noProfile
}

func newSession(kb Keyboard) *session { return &session{kb: kb, active: noProfile} }

// switchTo makes p the active profile. The switch is left out when the
// keyboard last confirmed p and the input that waits reports no change since.
// A failed switch leaves the active profile unknown. The error names p.
func (s *session) switchTo(p byte) error {
	if err := s.switchBare(p); err != nil {
		return fmt.Errorf("profile %d: %w", p, err)
	}
	return nil
}

// switchBare is switchTo with the keyboard's error as it is.
func (s *session) switchBare(p byte) error {
	if s.active == int(p) {
		s.follow()
		if s.active == int(p) {
			return nil
		}
	}
	known := s.active != noProfile
	s.active = noProfile
	var err error
	if known {
		err = s.kb.SwitchProfile(p)
	} else {
		err = s.kb.SwitchProfileFresh(p)
	}
	if err != nil {
		return err
	}
	s.active = int(p)
	return nil
}

// follow reads the input that waits, which the next switch would drop
// unread, and takes the active profile from its profile notifications: the
// profile may have been changed on the keyboard itself. Input that cannot be
// read, or more than maxEvents reports of it, leaves the profile unknown.
func (s *session) follow() {
	for range maxEvents {
		ev, err := s.kb.NextEvent(eventWait)
		if errors.Is(err, device.ErrTimeout) {
			return
		}
		if err != nil {
			s.active = noProfile
			return
		}
		if c, ok := ev.(proto.ProfileChanged); ok {
			s.active = int(c.Profile)
		}
	}
	s.active = noProfile
}

// job is one configured button with its rendered picture.
type job struct {
	o    ledger.Owner
	b    layout.Button
	data []byte
	hash string
}

// tables are a profile's tables as read in the first pass, with the
// restored positions already written into them. The second pass writes the
// whole type table back, so the restored positions keep their originals.
type tables struct {
	t29 *proto.Table29
	t25 *proto.Table25
}

func where(o ledger.Owner) string {
	return fmt.Sprintf("profile %d page %d position %d", o.Profile, o.Page, o.Position)
}

// Apply puts every layout of cfg on the keyboard.
//
// Every picture is rendered before the keyboard is touched, so a bad file or
// a title that does not fit stops Apply before anything is sent. Then Apply
// works in two passes over the profiles of cfg and those the ledger still
// holds positions in, in ascending order. The first pass reads each profile's
// tables, gives positions that are no longer in the layout their originals
// back, records the originals of positions taken over for the first time and
// stops trusting the slot of a position that no longer shows it; it ends by
// reading the 0x29 tables of the other profiles and keeping the slots of
// launchers kb7screen does not manage out of use (see avoidForeign). The second
// pass assigns slots, sends the pictures and writes the tables, profile by
// profile: the 0x29 table if an entry changed, then the whole 0x25 table,
// after which Apply waits for the keyboard's redraw before it switches to
// another profile. Removed positions have freed their slots before any is
// assigned, and a slot shared with a lower profile is never given to another
// picture while that profile still shows it. With full, every picture is sent
// again.
//
// Last, Apply sets the stored lighting (report 0x11) of every profile whose
// layout has [light] or [stored_light], in ascending order, where the
// keyboard holds other colours or another brightness than the layout asks
// for; see storeLighting. Other profiles' stored lighting is neither read
// nor written.
//
// Then it renames the profiles whose layout has a name that differs from the
// keyboard's, in one write of report 0x16; see rename.
//
// When it succeeds, Apply ends on the profile the run command would choose:
// the first one whose program is running, otherwise the desktop profile.
// running may be nil when no program is checked.
//
// Once ctx is cancelled, Apply stops before the next profile of either pass,
// or before the next picture. A profile whose pictures it stops between
// still gets its tables written for the pictures already sent, and a slot
// sent in this Apply that a position not yet written shows is no longer
// trusted, so the next Apply sends it again and redraws every position that
// shows it. Apply then returns ErrStopped.
//
// save, which may be nil, is called after the first pass, so the originals
// are stored before anything is overwritten, and after each profile of the
// second pass; an error from it stops Apply. The caller must also save after
// Apply returns, whether or not it failed.
func Apply(ctx context.Context, kb Keyboard, cfg *layout.Config, l *ledger.Ledger, full bool, running func(name string) bool, save func() error) (Report, error) {
	var rep Report
	jobs, err := renderAll(cfg)
	if err != nil {
		return rep, err
	}
	profiles, err := managed(cfg, l)
	if err != nil {
		return rep, err
	}
	if err := stopped(ctx); err != nil {
		return rep, err
	}
	if full {
		l.Forget()
	}
	if err := kb.EnableNotifications(); err != nil {
		return rep, err
	}
	s := newSession(kb)
	read := map[byte]tables{}
	for _, p := range profiles {
		if err := stopped(ctx); err != nil {
			return rep, err
		}
		t, err := prepare(s, l, p, jobs[p], &rep)
		if err != nil {
			return rep, err
		}
		read[p] = t
	}
	if err := avoidForeign(kb, l, jobs, read, &rep); err != nil {
		return rep, err
	}
	if err := saveLedger(save); err != nil {
		return rep, err
	}
	uploaded := map[byte]bool{}
	for i, p := range profiles {
		if len(jobs[p]) == 0 {
			continue
		}
		done, err := send(ctx, s, l, p, jobs[p], read[p], uploaded, &rep)
		if err != nil {
			// A stop leaves the jobs not taken unwritten; any other error
			// may have come before the tables of this profile were
			// written, so all of its jobs count as pending.
			first := 0
			if errors.Is(err, ErrStopped) {
				first = done
			}
			pending := append([]job(nil), jobs[p][first:]...)
			for _, q := range profiles[i+1:] {
				pending = append(pending, jobs[q]...)
			}
			distrust(l, uploaded, pending)
			return rep, err
		}
		if err := saveLedger(save); err != nil {
			return rep, err
		}
	}
	for _, p := range lit(cfg) {
		if err := stopped(ctx); err != nil {
			return rep, err
		}
		written, err := storeLighting(s, p)
		if err != nil {
			return rep, err
		}
		if written {
			rep.Lighting = append(rep.Lighting, p.Number)
		}
	}
	if err := stopped(ctx); err != nil {
		return rep, err
	}
	if err := rename(kb, cfg, &rep); err != nil {
		return rep, err
	}
	if err := stopped(ctx); err != nil {
		return rep, err
	}
	return rep, s.switchTo(finalProfile(cfg, running))
}

// rename gives every profile whose layout has a name that differs from the
// keyboard's that name, in one write of report 0x16 (the keyboard holds all
// five names in it, and the write needs no profile switch). Nothing is read
// when no layout has a name, and nothing written when every name already
// matches.
func rename(kb Keyboard, cfg *layout.Config, rep *Report) error {
	var named []layout.Profile
	for _, p := range cfg.Profiles {
		if p.Name != "" {
			named = append(named, p)
		}
	}
	if len(named) == 0 {
		return nil
	}
	sort.Slice(named, func(i, j int) bool { return named[i].Number < named[j].Number })
	t, err := kb.ReadNamesTable()
	if err != nil {
		return fmt.Errorf("profile names: %w", err)
	}
	var done []Rename
	for _, p := range named {
		if t.Name(int(p.Number)) == p.Name {
			continue
		}
		if err := t.SetName(int(p.Number), p.Name); err != nil {
			return fmt.Errorf("profile %d: %w", p.Number, err)
		}
		done = append(done, Rename{p.Number, p.Name})
	}
	if len(done) == 0 {
		return nil
	}
	if err := kb.WriteNamesTable(t); err != nil {
		return fmt.Errorf("profile names: %w", err)
	}
	rep.Renamed = append(rep.Renamed, done...)
	return nil
}

// lit returns the profiles of cfg with [light] or [stored_light], in
// ascending order.
func lit(cfg *layout.Config) []layout.Profile {
	var out []layout.Profile
	for _, p := range cfg.Profiles {
		if p.Light != nil || p.StoredLight != nil {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out
}

// storeLighting reads the stored lighting of p and writes it back when it
// differs from what the layout asks for, switching to p first. When the
// table cannot be read while another profile is active (reading the table
// of an inactive profile is not measured), it switches to p and reads it
// again. The colours
// asked for are the [stored_light] colour on every LED when it gives one,
// else the still picture of the [light] layers (light.Still), else the ones
// the keyboard holds; the brightness is the [stored_light] one when given,
// else the keyboard's. Every byte the table does not interpret is written
// back as read. It reports whether it wrote.
func storeLighting(s *session, p layout.Profile) (bool, error) {
	t, err := s.kb.ReadLightsTable(p.Number)
	if err != nil {
		if err := s.switchBare(p.Number); err != nil {
			return false, fmt.Errorf("profile %d: stored lighting: %w", p.Number, err)
		}
		if t, err = s.kb.ReadLightsTable(p.Number); err != nil {
			return false, fmt.Errorf("profile %d: %w", p.Number, err)
		}
	}
	have := t.Colours()
	colours, brightness := have, t.Brightness()
	sl := p.StoredLight
	switch {
	case sl != nil && sl.Color.A != 0:
		colours = new(proto.LightPayload)
		for led := range light.NumLEDs {
			proto.SetLED(colours, led, sl.Color.R, sl.Color.G, sl.Color.B)
		}
	case p.Light != nil:
		colours = light.Still(p.Light)
	}
	if sl != nil && sl.Brightness >= 0 {
		brightness = sl.Brightness
	}
	if *colours == *have && brightness == t.Brightness() {
		return false, nil
	}
	t.SetColours(colours)
	if err := t.SetBrightness(brightness); err != nil {
		return false, fmt.Errorf("profile %d: %w", p.Number, err)
	}
	if err := s.switchBare(p.Number); err != nil {
		return false, fmt.Errorf("profile %d: stored lighting: %w", p.Number, err)
	}
	if err := s.kb.WriteLightsTable(t); err != nil {
		return false, fmt.Errorf("profile %d: stored lighting: %w", p.Number, err)
	}
	return true, nil
}

// avoidForeign ends the first pass. It reads the 0x29 table of every profile
// not read yet; a table is read for any profile without switching to it.
// Every slot shown by an application button that the ledger does not own
// and that the layouts do not take over belongs to a launcher kb7screen does
// not manage, on any page: the ledger is told to leave such a slot alone, with
// a note. When the layouts' pictures do not fit in the slots that are left,
// avoidForeign refuses, naming the launchers, before any picture is uploaded
// (the first pass may already have restored removed positions).
func avoidForeign(kb Keyboard, l *ledger.Ledger, jobs map[byte][]job, read map[byte]tables, rep *Report) error {
	taken := map[ledger.Owner]bool{}
	hashes := map[string]bool{}
	for _, js := range jobs {
		for _, j := range js {
			taken[j.o] = true
			hashes[j.hash] = true
		}
	}
	shownBy := map[byte][]ledger.Owner{}
	for p := byte(0); p <= maxProfile; p++ {
		t29 := read[p].t29
		if t29 == nil {
			var err error
			if t29, err = kb.ReadButtons(p); err != nil {
				return fmt.Errorf("profile %d: %w", p, err)
			}
		}
		for _, o := range places(int(p)) {
			i, _ := proto.Entry29Index(o.Page, o.Position)
			e := t29.Entry(i)
			if e.Kind != proto.KindApp || e.Slot < 1 || e.Slot > ledger.MaxSlot || taken[o] {
				continue
			}
			if _, owned := l.SlotOf(o); owned {
				continue
			}
			shownBy[e.Slot] = append(shownBy[e.Slot], o)
		}
	}
	var launchers []string
	for n := byte(1); n <= ledger.MaxSlot; n++ {
		if len(shownBy[n]) == 0 {
			continue
		}
		l.Block(n)
		var at []string
		for _, o := range shownBy[n] {
			at = append(at, where(o))
		}
		launchers = append(launchers, at...)
		rep.Notes = append(rep.Notes, fmt.Sprintf("slot %d is shown by %s, which kb7screen does not manage; it is left alone", n, strings.Join(at, " and ")))
	}
	if free := ledger.MaxSlot - len(shownBy); len(hashes) > free {
		return fmt.Errorf("the layouts hold %d different pictures, but launchers that kb7screen does not manage show %d of the %d slots (%s); empty them with kb7screen clear-launchers: %w",
			len(hashes), len(shownBy), ledger.MaxSlot, strings.Join(launchers, ", "), ledger.ErrNoSlot)
	}
	return nil
}

// distrust forgets the content of every slot sent in this Apply that one of
// the pending jobs, whose positions may not have been written, still uses: those
// positions were not redrawn, and the next Apply must send the slot again to
// redraw them.
func distrust(l *ledger.Ledger, uploaded map[byte]bool, pending []job) {
	for _, j := range pending {
		if n, ok := l.SlotOf(j.o); ok && uploaded[n] {
			l.ForgetSlot(n)
		}
	}
}

func saveLedger(save func() error) error {
	if save == nil {
		return nil
	}
	if err := save(); err != nil {
		return fmt.Errorf("saving the ledger: %w", err)
	}
	return nil
}

// renderAll renders every button, sorted per profile by page and position,
// and refuses layouts that need more pictures than there are slots.
func renderAll(cfg *layout.Config) (map[byte][]job, error) {
	jobs := map[byte][]job{}
	hashes := map[string]bool{}
	for _, p := range cfg.Profiles {
		for _, b := range p.Buttons {
			data, err := render(b)
			if err != nil {
				return nil, fmt.Errorf("%s: page %d position %d: %w", filepath.Base(p.File), b.Page, b.Position, err)
			}
			h := picture.Hash(data)
			hashes[h] = true
			o := ledger.Owner{Profile: int(p.Number), Page: b.Page, Position: b.Position}
			jobs[p.Number] = append(jobs[p.Number], job{o: o, b: b, data: data, hash: h})
		}
		js := jobs[p.Number]
		sort.Slice(js, func(i, j int) bool {
			if js[i].o.Page != js[j].o.Page {
				return js[i].o.Page < js[j].o.Page
			}
			return js[i].o.Position < js[j].o.Position
		})
	}
	if len(hashes) > ledger.MaxSlot {
		return nil, fmt.Errorf("the layouts hold %d different pictures and there are %d slots: %w", len(hashes), ledger.MaxSlot, ledger.ErrNoSlot)
	}
	return jobs, nil
}

// render returns the encoded picture of a button.
func render(b layout.Button) ([]byte, error) {
	img, err := RenderButton(b)
	if err != nil {
		return nil, err
	}
	return picture.Encode(img)
}

// RenderButton draws a button's picture: its own picture fitted, or the
// built-in style with its symbol and title. Apply sends exactly this image.
func RenderButton(b layout.Button) (*image.NRGBA, error) {
	if b.Picture != "" {
		src, err := loadPNG(b.Picture)
		if err != nil {
			return nil, err
		}
		return picture.Fit(src), nil
	}
	var sym image.Image
	if b.Symbol != "" {
		s, err := loadPNG(b.Symbol)
		if err != nil {
			return nil, err
		}
		sym = s
	}
	return picture.Render(picture.Style{Color: b.Color, Symbol: sym, Title: b.Title})
}

func loadPNG(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return img, nil
}

// managed returns, in ascending order, the profiles of cfg and those the
// ledger still holds positions in. It refuses a ledger that lists a position
// kb7screen does not manage, before anything is sent.
func managed(cfg *layout.Config, l *ledger.Ledger) ([]byte, error) {
	set := map[byte]bool{}
	for _, p := range cfg.Profiles {
		set[p.Number] = true
	}
	for p := 0; p <= maxProfile; p++ {
		owners := l.Owners(p)
		if err := checkOwners(owners); err != nil {
			return nil, err
		}
		if len(owners) > 0 {
			set[byte(p)] = true
		}
	}
	out := make([]byte, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func checkOwners(owners []ledger.Owner) error {
	for _, o := range owners {
		if _, _, err := indexes(o); err != nil {
			return fmt.Errorf("the ledger lists %s, which kb7screen does not manage", where(o))
		}
	}
	return nil
}

// indexes returns the 0x29 and 0x25 entries of a position.
func indexes(o ledger.Owner) (i29, i25 int, err error) {
	if i29, err = proto.Entry29Index(o.Page, o.Position); err != nil {
		return 0, 0, err
	}
	if i25, err = proto.TypeIndex(o.Page, o.Position); err != nil {
		return 0, 0, err
	}
	return i29, i25, nil
}

// places lists every position of a profile that has a 0x29 entry, page by
// page.
func places(profile int) []ledger.Owner {
	var out []ledger.Owner
	for pg := 1; pg <= proto.Pages; pg++ {
		for pos := 0; pos < proto.PagePositions; pos++ {
			if _, err := proto.Entry29Index(pg, pos); err == nil {
				out = append(out, ledger.Owner{Profile: profile, Page: pg, Position: pos})
			}
		}
	}
	return out
}

// showsSlot reports whether e is an application button showing slot n.
func showsSlot(e proto.Entry29, n byte) bool {
	want := proto.AppEntry(n, "")
	return e.Mod == want.Mod && e.Key == want.Key && e.Kind == want.Kind && e.Slot == want.Slot
}

// prepare is the first pass over one profile: it reads the tables, restores
// and releases the positions no longer in the layout, records originals and
// stops trusting slots that a position no longer shows. Nothing else is
// written.
func prepare(s *session, l *ledger.Ledger, p byte, jobs []job, rep *Report) (tables, error) {
	if err := s.switchTo(p); err != nil {
		return tables{}, err
	}
	t29, err := s.kb.ReadButtons(p)
	if err != nil {
		return tables{}, fmt.Errorf("profile %d: %w", p, err)
	}
	t25, err := s.kb.ReadTypes(p)
	if err != nil {
		return tables{}, fmt.Errorf("profile %d: %w", p, err)
	}

	inLayout := map[ledger.Owner]bool{}
	for _, j := range jobs {
		inLayout[j.o] = true
	}
	var gone []ledger.Owner
	for _, o := range l.Owners(int(p)) {
		if !inLayout[o] {
			gone = append(gone, o)
		}
	}
	if err := restore(s.kb, l, t29, t25, gone, rep); err != nil {
		return tables{}, fmt.Errorf("profile %d: %w", p, err)
	}

	// A position that does not show the slot the ledger gives it was changed
	// by someone else, who may have reused the slot as well, or an earlier
	// apply stopped before writing it: the slot's picture is not trusted.
	for _, j := range jobs {
		i29, i25, err := indexes(j.o)
		if err != nil {
			return tables{}, fmt.Errorf("%s: %w", where(j.o), err)
		}
		cur := t29.Entry(i29)
		l.RecordOriginal(j.o, ledger.Original{Entry: cur, Type: t25.Type(i25)})
		if n, ok := l.SlotOf(j.o); ok && !showsSlot(cur, n) {
			l.ForgetSlot(n)
			rep.Notes = append(rep.Notes, fmt.Sprintf("%s does not show slot %d; its picture is sent again", where(j.o), n))
		}
	}
	return tables{t29, t25}, nil
}

// send is the second pass over one profile: it assigns the slots, sends the
// pictures that are not in them, writes the 0x29 table once if an entry
// changed and the whole 0x25 table when a position is to be redrawn. It
// switches to the profile only when there is something to send. uploaded
// collects the slots
// sent during this Apply, so every position showing one is redrawn.
//
// Once ctx is cancelled, send takes no further job, writes the tables for
// the jobs it has taken and returns ErrStopped. It returns the number of jobs
// taken; the positions of the others are left as they are.
func send(ctx context.Context, s *session, l *ledger.Ledger, p byte, jobs []job, t tables, uploaded map[byte]bool, rep *Report) (int, error) {
	var stop error
	slots := make([]byte, 0, len(jobs))
	for _, j := range jobs {
		if stop = stopped(ctx); stop != nil {
			break
		}
		slot, up, err := l.Assign(j.o, j.hash)
		if err != nil {
			return len(slots), fmt.Errorf("%s: %w", where(j.o), err)
		}
		if up {
			err = s.switchTo(p)
			if err == nil {
				if err = s.kb.Upload(slot, j.data); err != nil {
					err = fmt.Errorf("%s, slot %d: %w", where(j.o), slot, err)
				}
			}
			if err != nil {
				// The slot does not hold the picture, or not all of it. The
				// position stays managed, as the keyboard may still show the
				// slot there, and the next apply sends the picture again.
				l.ForgetSlot(slot)
				return len(slots), err
			}
			uploaded[slot] = true
			rep.Uploaded = append(rep.Uploaded, j.o)
		} else {
			rep.Skipped = append(rep.Skipped, j.o)
		}
		slots = append(slots, slot)
	}
	done := len(slots)
	if err := write(s, p, jobs[:done], slots, t, uploaded); err != nil {
		return done, err
	}
	return done, stop
}

// write is the end of send: it writes the 0x29 table once if the entry of a
// job changed, and then the whole 0x25 table, with every job's position an
// application button and every other position as read, if a position is to
// be redrawn: its entry or type changed, or the slot it shows was sent in
// this Apply. The keyboard redraws once it has taken the 0x25 table over;
// write waits for that, so that no later switch moves a queued change to
// another profile. Nothing stops it half way but a keyboard error.
//
// The per-position type report (0x35) is not used: it names no profile and
// the keyboard queues it, so a switch sent after it can change the wrong
// profile.
func write(s *session, p byte, jobs []job, slots []byte, t tables, uploaded map[byte]bool) error {
	changed := false
	redraw := false
	for i, j := range jobs {
		pos, tpos, err := indexes(j.o)
		if err != nil {
			return fmt.Errorf("%s: %w", where(j.o), err)
		}
		want := proto.AppEntry(slots[i], j.b.Label)
		entryChanged := t.t29.Entry(pos) != want
		if entryChanged {
			if err := t.t29.SetEntry(pos, want); err != nil {
				return fmt.Errorf("%s: %w", where(j.o), err)
			}
			changed = true
		}
		if t.t25.Type(tpos) != proto.AppType {
			if err := t.t25.SetType(tpos, proto.AppType); err != nil {
				return fmt.Errorf("%s: %w", where(j.o), err)
			}
			redraw = true
		}
		if entryChanged || uploaded[slots[i]] {
			redraw = true
		}
	}
	if !redraw {
		return nil
	}
	if err := s.switchTo(p); err != nil {
		return err
	}
	if changed {
		if err := s.kb.WriteButtons(t.t29); err != nil {
			return fmt.Errorf("profile %d: %w", p, err)
		}
	}
	if err := writeTypes(s.kb, t.t25); err != nil {
		return fmt.Errorf("profile %d: %w", p, err)
	}
	return nil
}

// writeTypes writes a whole type table and waits for the keyboard to redraw,
// which says it has taken the table over. Until then a profile switch could
// move the change to another profile, so an error leaves the caller to stop
// without switching.
func writeTypes(kb Keyboard, t25 *proto.Table25) error {
	if err := kb.WriteTypes(t25); err != nil {
		return err
	}
	if err := kb.WaitRedraw(redrawTimeout); err != nil {
		return fmt.Errorf("the type table was written but the keyboard did not redraw: %w", err)
	}
	return nil
}

// finalProfile is the profile the run command would choose.
func finalProfile(cfg *layout.Config, running func(string) bool) byte {
	if running != nil {
		ps := append([]layout.Profile(nil), cfg.Profiles...)
		sort.Slice(ps, func(i, j int) bool { return ps[i].Number < ps[j].Number })
		for _, p := range ps {
			if p.SwitchWhenRunning != "" && running(p.SwitchWhenRunning) {
				return p.Number
			}
		}
	}
	return cfg.DesktopProfile()
}

// Restore gives the positions the ledger lists for profile their recorded
// original contents, releases them and drops their originals. Positions
// without a recorded original are emptied. It switches to profile to do so
// and does not switch back.
//
// Restore works on one profile, whose tables are written together, so a
// cancelled ctx stops it only before it touches the keyboard; it then returns
// ErrStopped and nothing was changed.
//
// save, which may be nil, is called once the positions are restored; an error
// from it is returned. The caller must also save after Restore returns,
// whether or not it failed.
func Restore(ctx context.Context, kb Keyboard, l *ledger.Ledger, profile byte, save func() error) (Report, error) {
	var rep Report
	if profile > maxProfile {
		return rep, fmt.Errorf("profile %d: profiles are 0 to %d", profile, maxProfile)
	}
	owners := l.Owners(int(profile))
	if err := checkOwners(owners); err != nil {
		return rep, err
	}
	if len(owners) == 0 {
		rep.Notes = append(rep.Notes, fmt.Sprintf("the ledger lists no positions of profile %d; nothing was changed", profile))
		return rep, nil
	}
	if err := stopped(ctx); err != nil {
		return rep, err
	}
	if err := kb.EnableNotifications(); err != nil {
		return rep, err
	}
	if err := newSession(kb).switchTo(profile); err != nil {
		return rep, err
	}
	t29, err := kb.ReadButtons(profile)
	if err != nil {
		return rep, fmt.Errorf("profile %d: %w", profile, err)
	}
	t25, err := kb.ReadTypes(profile)
	if err != nil {
		return rep, fmt.Errorf("profile %d: %w", profile, err)
	}
	if err := restore(kb, l, t29, t25, owners, &rep); err != nil {
		return rep, fmt.Errorf("profile %d: %w", profile, err)
	}
	if err := saveLedger(save); err != nil {
		return rep, err
	}
	return rep, nil
}

// restore writes the originals of owners, checked by checkOwners, into the
// profile's 0x29 and then its whole 0x25, and waits for the keyboard's
// redraw. Once the keyboard has redrawn, it releases
// the positions and drops their originals, so a later takeover records what
// they hold then. An original label the table cannot hold is left empty,
// with a note.
func restore(kb Keyboard, l *ledger.Ledger, t29 *proto.Table29, t25 *proto.Table25, owners []ledger.Owner, rep *Report) error {
	if len(owners) == 0 {
		return nil
	}
	for _, o := range owners {
		i29, i25, err := indexes(o)
		if err != nil {
			return fmt.Errorf("%s: %w", where(o), err)
		}
		orig, _ := l.Original(o) // without one: an all-zero entry and type 00/00
		e := orig.Entry
		if err := t29.SetEntry(i29, e); err != nil {
			e.Label = ""
			if err2 := t29.SetEntry(i29, e); err2 != nil {
				return errors.Join(err, err2)
			}
			rep.Notes = append(rep.Notes, fmt.Sprintf("%s: the original label %q cannot be written back; it is left empty", where(o), orig.Entry.Label))
		}
		if err := t25.SetType(i25, orig.Type); err != nil {
			return err
		}
	}
	if err := kb.WriteButtons(t29); err != nil {
		return err
	}
	if err := writeTypes(kb, t25); err != nil {
		return err
	}
	for _, o := range owners {
		l.Release(o)
		l.ForgetOriginal(o)
		rep.Restored = append(rep.Restored, o)
	}
	return nil
}
