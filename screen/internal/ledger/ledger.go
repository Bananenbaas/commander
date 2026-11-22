// SPDX-License-Identifier: GPL-2.0-or-later

// Package ledger remembers what each picture slot on the keyboard holds, so
// unchanged pictures are never uploaded twice, and keeps the original table
// entries of every position kb7screen has taken over.
package ledger

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/Bananenbaas/commander/screen/internal/proto"
)

// MaxSlot is the highest picture slot. Slots 9-15 were seen taking uploads
// (40-swarm-slots-pages-1); Swarm II itself allows 15 application buttons.
const MaxSlot = 15

// ErrNoSlot is returned by Assign when every slot is in use.
var ErrNoSlot = errors.New("ledger: no free picture slot")

// Owner identifies a button position.
type Owner struct{ Profile, Page, Position int }

func (o Owner) String() string { return fmt.Sprintf("p%d/%d/%d", o.Profile, o.Page, o.Position) }

func parseOwner(s string) (Owner, error) {
	var o Owner
	if _, err := fmt.Sscanf(s, "p%d/%d/%d", &o.Profile, &o.Page, &o.Position); err != nil {
		return Owner{}, fmt.Errorf("ledger: bad owner %q", s)
	}
	return o, nil
}

// Original is the table content of a position before it was taken over.
type Original struct {
	Entry proto.Entry29
	Type  proto.TypeEntry
}

// slotState is a slot the ledger knows something about: its users, its
// content, or both. A slot without users whose content is known is free but
// still holds that picture, so a button moved to another position finds it
// again without an upload. A slot the ledger has no state for is free and
// its content is unknown.
type slotState struct {
	hash  string // "" when unknown
	users map[Owner]bool
}

// Ledger is the in-memory slot ledger. The zero value is an empty ledger.
type Ledger struct {
	slots     map[byte]*slotState
	originals map[Owner]Original
	blocked   map[byte]bool // slots Assign must not use; not saved
}

// SlotOf returns the slot an owner currently uses.
func (l *Ledger) SlotOf(o Owner) (byte, bool) {
	for n, s := range l.slots {
		if s.users[o] {
			return n, true
		}
	}
	return 0, false
}

func (l *Ledger) leave(o Owner) {
	n, ok := l.SlotOf(o)
	if !ok {
		return
	}
	s := l.slots[n]
	delete(s.users, o)
	l.drop(n)
}

// drop removes the state of slot n once it has neither users nor a known
// content.
func (l *Ledger) drop(n byte) {
	if s := l.slots[n]; s != nil && len(s.users) == 0 && s.hash == "" {
		delete(l.slots, n)
	}
}

func (l *Ledger) join(n byte, o Owner, hash string) {
	if l.slots == nil {
		l.slots = map[byte]*slotState{}
	}
	s := l.slots[n]
	if s == nil {
		s = &slotState{users: map[Owner]bool{}}
		l.slots[n] = s
	}
	s.hash = hash
	s.users[o] = true
}

// keep records that the free slot n holds the picture with the given hash.
func (l *Ledger) keep(n byte, hash string) {
	if l.slots == nil {
		l.slots = map[byte]*slotState{}
	}
	l.slots[n] = &slotState{hash: hash, users: map[Owner]bool{}}
}

// Assign picks the slot for o's picture with the given hash and reports
// whether the picture has to be uploaded.
//
// A slot that holds the picture already, in use or free, is taken without
// an upload. Otherwise o keeps its own slot if no one else uses it, or gets
// a new one: a free slot of unknown content first, then a free slot whose
// content is known, which that content then loses.
func (l *Ledger) Assign(o Owner, hash string) (slot byte, upload bool, err error) {
	cur, owns := l.SlotOf(o)
	if owns && l.blocked[cur] {
		owns = false
	}
	if owns && hash != "" && l.slots[cur].hash == hash {
		return cur, false, nil
	}
	if hash != "" {
		for n := byte(1); n <= MaxSlot; n++ {
			if s := l.slots[n]; s != nil && s.hash == hash && !l.blocked[n] {
				l.leave(o)
				l.join(n, o, hash)
				return n, false, nil
			}
		}
	}
	if owns && (len(l.slots[cur].users) == 1 || l.slots[cur].hash == "") {
		l.slots[cur].hash = hash
		return cur, true, nil
	}
	for _, free := range []func(*slotState) bool{
		func(s *slotState) bool { return s == nil },
		func(s *slotState) bool { return s != nil && len(s.users) == 0 },
	} {
		for n := byte(1); n <= MaxSlot; n++ {
			if free(l.slots[n]) && !l.blocked[n] {
				l.leave(o)
				l.join(n, o, hash)
				return n, true, nil
			}
		}
	}
	return 0, false, ErrNoSlot
}

// Release removes o from its slot. A slot without users becomes free and
// keeps its known content.
func (l *Ledger) Release(o Owner) { l.leave(o) }

// Block keeps slot n out of every later Assign in this ledger: it is never
// matched, given out or uploaded to, and an owner that uses it moves to
// another slot. The slot's content is no longer trusted. Blocks are not
// saved.
func (l *Ledger) Block(n byte) {
	if l.blocked == nil {
		l.blocked = map[byte]bool{}
	}
	l.blocked[n] = true
	l.ForgetSlot(n)
}

// Blocked reports whether slot n is blocked.
func (l *Ledger) Blocked(n byte) bool { return l.blocked[n] }

// RecordOriginal stores orig unless an original is already recorded for o.
func (l *Ledger) RecordOriginal(o Owner, orig Original) {
	if _, ok := l.originals[o]; ok {
		return
	}
	if l.originals == nil {
		l.originals = map[Owner]Original{}
	}
	l.originals[o] = orig
}

// ForgetOriginal drops the recorded original of o, so the next takeover of
// the position records what it holds then.
func (l *Ledger) ForgetOriginal(o Owner) { delete(l.originals, o) }

// Original returns the recorded original of o.
func (l *Ledger) Original(o Owner) (Original, bool) {
	orig, ok := l.originals[o]
	return orig, ok
}

// Forget clears every slot's hash but keeps users and slot numbers. Free
// slots become slots of unknown content.
func (l *Ledger) Forget() {
	for n := range l.slots {
		l.ForgetSlot(n)
	}
}

// ForgetSlot clears the hash of slot n but keeps its users, so the next
// Assign of a picture to it uploads again. A free slot becomes a slot of
// unknown content.
func (l *Ledger) ForgetSlot(n byte) {
	if s := l.slots[n]; s != nil {
		s.hash = ""
		l.drop(n)
	}
}

// Owners returns the owners of the given profile that hold a slot, sorted by
// page and position.
func (l *Ledger) Owners(profile int) []Owner {
	m := map[Owner]bool{}
	for _, s := range l.slots {
		for o := range s.users {
			if o.Profile == profile {
				m[o] = true
			}
		}
	}
	return sortedOwners(m)
}

type fileSlot struct {
	Slot  byte     `json:"slot"`
	Hash  string   `json:"hash"`
	Users []string `json:"users"`
}

type fileOriginal struct {
	Owner string          `json:"owner"`
	Entry proto.Entry29   `json:"entry"`
	Type  proto.TypeEntry `json:"type"`
}

type fileFormat struct {
	Slots     []fileSlot     `json:"slots"`
	Originals []fileOriginal `json:"originals"`
}

// Load reads a ledger file. A missing file gives an empty ledger; an
// unreadable one is an error.
func Load(path string) (*Ledger, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Ledger{}, nil
	}
	if err != nil {
		return nil, err
	}
	var f fileFormat
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("ledger: %s: %w", path, err)
	}
	l := &Ledger{}
	for _, fs := range f.Slots {
		if fs.Slot < 1 || fs.Slot > MaxSlot {
			return nil, fmt.Errorf("ledger: %s: slot %d out of range", path, fs.Slot)
		}
		if len(fs.Users) == 0 && fs.Hash != "" {
			// A free slot whose content is known. Older versions never
			// write one, and skip it when reading.
			l.keep(fs.Slot, fs.Hash)
		}
		for _, u := range fs.Users {
			o, err := parseOwner(u)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			l.join(fs.Slot, o, fs.Hash)
		}
	}
	for _, fo := range f.Originals {
		o, err := parseOwner(fo.Owner)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		l.RecordOriginal(o, Original{Entry: fo.Entry, Type: fo.Type})
	}
	return l, nil
}

// Save writes the ledger atomically (temporary file, then rename).
func (l *Ledger) Save(path string) error {
	f := fileFormat{Slots: []fileSlot{}, Originals: []fileOriginal{}}
	nums := make([]int, 0, len(l.slots))
	for n := range l.slots {
		nums = append(nums, int(n))
	}
	sort.Ints(nums)
	for _, n := range nums {
		s := l.slots[byte(n)]
		fs := fileSlot{Slot: byte(n), Hash: s.hash, Users: []string{}}
		for _, o := range sortedOwners(s.users) {
			fs.Users = append(fs.Users, o.String())
		}
		f.Slots = append(f.Slots, fs)
	}
	keys := make(map[Owner]bool, len(l.originals))
	for o := range l.originals {
		keys[o] = true
	}
	for _, o := range sortedOwners(keys) {
		orig := l.originals[o]
		f.Originals = append(f.Originals, fileOriginal{Owner: o.String(), Entry: orig.Entry, Type: orig.Type})
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), ".ledger-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

func sortedOwners(m map[Owner]bool) []Owner {
	out := make([]Owner, 0, len(m))
	for o := range m {
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Profile != b.Profile {
			return a.Profile < b.Profile
		}
		if a.Page != b.Page {
			return a.Page < b.Page
		}
		return a.Position < b.Position
	})
	return out
}
