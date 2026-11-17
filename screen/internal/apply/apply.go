// Package apply puts the configured layouts on the keyboard and gives the
// positions kb7screen has taken over back their original contents.
//
// Nothing is retried: the first keyboard error stops the work and is returned
// with its context. The ledger is changed in memory only; saving it is up to
// the caller.
package apply

import (
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"

	"github.com/Bananenbaas/kb7-commander/screen/internal/layout"
	"github.com/Bananenbaas/kb7-commander/screen/internal/ledger"
	"github.com/Bananenbaas/kb7-commander/screen/internal/picture"
	"github.com/Bananenbaas/kb7-commander/screen/internal/proto"
)

// Keyboard holds the keyboard operations apply and restore use. The
// keyboard package's *KB provides them.
type Keyboard interface {
	EnableNotifications() error
	SwitchProfile(p byte) error
	ReadButtons(p byte) (*proto.Table29, error)
	ReadTypes(p byte) (*proto.Table25, error)
	Upload(slot byte, data []byte) error
	WriteButtons(t *proto.Table29) error
	WriteType(pos byte, te proto.TypeEntry) error
	WriteTypes(t *proto.Table25) error
}

// Report says what Apply or Restore did.
type Report struct {
	Uploaded, Skipped []ledger.Owner // buttons whose picture was sent, or was already in its slot
	Restored          []ledger.Owner // positions given back their original contents
	Notes             []string       // things the user should know about
}

const (
	maxProfile  = 4
	page        = 1  // the only page that is measured
	maxPosition = 11 // positions of page 1
)

// job is one configured button with its rendered picture.
type job struct {
	o    ledger.Owner
	b    layout.Button
	data []byte
	hash string
}

func where(o ledger.Owner) string {
	return fmt.Sprintf("profile %d page %d position %d", o.Profile, o.Page, o.Position)
}

// Apply puts every layout of cfg on the keyboard.
//
// Every picture is rendered before the keyboard is touched, so a bad file or
// a title that does not fit stops Apply before anything is sent. With full,
// every picture is sent again. Apply ends on the profile the run command
// would choose: the first one whose program is running, otherwise the
// desktop profile. running may be nil when no program is checked.
func Apply(kb Keyboard, cfg *layout.Config, l *ledger.Ledger, full bool, running func(name string) bool) (Report, error) {
	var rep Report
	jobs, err := renderAll(cfg)
	if err != nil {
		return rep, err
	}
	profiles, err := managed(cfg, l)
	if err != nil {
		return rep, err
	}
	if full {
		l.Forget()
	}
	if err := kb.EnableNotifications(); err != nil {
		return rep, err
	}
	uploaded := map[byte]bool{}
	for _, p := range profiles {
		if err := applyProfile(kb, l, p, jobs[p], uploaded, &rep); err != nil {
			return rep, err
		}
	}
	final := finalProfile(cfg, running)
	if err := kb.SwitchProfile(final); err != nil {
		return rep, fmt.Errorf("profile %d: %w", final, err)
	}
	return rep, nil
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

// render returns the encoded picture of a button: its own picture fitted, or
// the built-in style with its symbol and title.
func render(b layout.Button) ([]byte, error) {
	var img *image.NRGBA
	if b.Picture != "" {
		src, err := loadPNG(b.Picture)
		if err != nil {
			return nil, err
		}
		img = picture.Fit(src)
	} else {
		var sym image.Image
		if b.Symbol != "" {
			s, err := loadPNG(b.Symbol)
			if err != nil {
				return nil, err
			}
			sym = s
		}
		r, err := picture.Render(picture.Style{Color: b.Color, Symbol: sym, Title: b.Title})
		if err != nil {
			return nil, err
		}
		img = r
	}
	return picture.Encode(img)
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
		if o.Page != page || o.Position < 0 || o.Position > maxPosition {
			return fmt.Errorf("the ledger lists %s, which kb7screen does not manage", where(o))
		}
	}
	return nil
}

// showsSlot reports whether e is an application button showing slot n.
func showsSlot(e proto.Entry29, n byte) bool {
	want := proto.AppEntry(n, "")
	return e.Mod == want.Mod && e.Key == want.Key && e.Kind == want.Kind && e.Slot == want.Slot
}

// applyProfile brings one profile in line with its jobs. uploaded collects
// the slots sent during this Apply, so every position showing one is redrawn.
func applyProfile(kb Keyboard, l *ledger.Ledger, p byte, jobs []job, uploaded map[byte]bool, rep *Report) error {
	if err := kb.SwitchProfile(p); err != nil {
		return fmt.Errorf("profile %d: %w", p, err)
	}
	t29, err := kb.ReadButtons(p)
	if err != nil {
		return fmt.Errorf("profile %d: %w", p, err)
	}
	t25, err := kb.ReadTypes(p)
	if err != nil {
		return fmt.Errorf("profile %d: %w", p, err)
	}

	// Positions managed before but no longer in the layout go first, which
	// also frees their slots for the new pictures.
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
	if err := restore(kb, l, t29, t25, gone, rep); err != nil {
		return fmt.Errorf("profile %d: %w", p, err)
	}

	// A position that no longer shows the slot the ledger gives it was
	// changed by someone else, who may have reused the slot as well: its
	// picture is not trusted.
	for _, j := range jobs {
		cur := t29.Entry(j.o.Position)
		l.RecordOriginal(j.o, ledger.Original{Entry: cur, Type: t25.Type(j.o.Position)})
		if n, ok := l.SlotOf(j.o); ok && !showsSlot(cur, n) {
			l.ForgetSlot(n)
			rep.Notes = append(rep.Notes, fmt.Sprintf("%s no longer showed slot %d; its picture is sent again", where(j.o), n))
		}
	}

	slots := make([]byte, len(jobs))
	for i, j := range jobs {
		slot, up, err := l.Assign(j.o, j.hash)
		if err != nil {
			return fmt.Errorf("%s: %w", where(j.o), err)
		}
		slots[i] = slot
		if !up {
			rep.Skipped = append(rep.Skipped, j.o)
			continue
		}
		if err := kb.Upload(slot, j.data); err != nil {
			// What the slot holds is unknown now.
			l.ForgetSlot(slot)
			l.Release(j.o)
			return fmt.Errorf("%s, slot %d: %w", where(j.o), slot, err)
		}
		uploaded[slot] = true
		rep.Uploaded = append(rep.Uploaded, j.o)
	}

	changed := false
	var redraw []byte
	for i, j := range jobs {
		pos := j.o.Position
		want := proto.AppEntry(slots[i], j.b.Label)
		entryChanged := t29.Entry(pos) != want
		if entryChanged {
			if err := t29.SetEntry(pos, want); err != nil {
				return fmt.Errorf("%s: %w", where(j.o), err)
			}
			changed = true
		}
		if entryChanged || t25.Type(pos) != proto.AppType || uploaded[slots[i]] {
			redraw = append(redraw, byte(pos))
		}
	}
	if changed {
		if err := kb.WriteButtons(t29); err != nil {
			return fmt.Errorf("profile %d: %w", p, err)
		}
	}
	for _, pos := range redraw {
		if err := kb.WriteType(pos, proto.AppType); err != nil {
			return fmt.Errorf("profile %d position %d: %w", p, pos, err)
		}
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
// original contents and releases them. Positions without a recorded original
// are emptied. It switches to profile to do so and does not switch back.
func Restore(kb Keyboard, l *ledger.Ledger, profile byte) (Report, error) {
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
	if err := kb.EnableNotifications(); err != nil {
		return rep, err
	}
	if err := kb.SwitchProfile(profile); err != nil {
		return rep, fmt.Errorf("profile %d: %w", profile, err)
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
	return rep, nil
}

// restore writes the originals of owners, checked by checkOwners, into the
// profile's 0x29 and then its whole 0x25, and releases them once both are
// written. An original label the table cannot hold is left empty, with a note.
func restore(kb Keyboard, l *ledger.Ledger, t29 *proto.Table29, t25 *proto.Table25, owners []ledger.Owner, rep *Report) error {
	if len(owners) == 0 {
		return nil
	}
	for _, o := range owners {
		orig, _ := l.Original(o) // without one: an all-zero entry and type 00/00
		e := orig.Entry
		if err := t29.SetEntry(o.Position, e); err != nil {
			e.Label = ""
			if err2 := t29.SetEntry(o.Position, e); err2 != nil {
				return errors.Join(err, err2)
			}
			rep.Notes = append(rep.Notes, fmt.Sprintf("%s: the original label %q cannot be written back; it is left empty", where(o), orig.Entry.Label))
		}
		if err := t25.SetType(o.Position, orig.Type); err != nil {
			return err
		}
	}
	if err := kb.WriteButtons(t29); err != nil {
		return err
	}
	if err := kb.WriteTypes(t25); err != nil {
		return err
	}
	for _, o := range owners {
		l.Release(o)
		rep.Restored = append(rep.Restored, o)
	}
	return nil
}
