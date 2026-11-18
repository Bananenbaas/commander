// SPDX-License-Identifier: GPL-2.0-or-later

package apply

import (
	"context"
	"fmt"

	"github.com/Bananenbaas/kb7-commander/screen/internal/ledger"
	"github.com/Bananenbaas/kb7-commander/screen/internal/proto"
)

// Kinds and types of the buttons Swarm II makes for launching things, as
// seen in 40-swarm-slots-pages-1: an application (0x29 kind 0x0b, type
// 05/06), a website (type 05/05; its 0x29 entry starts 07 0b 00) and a
// third-party application (type 0x42).
const (
	typeThirdApp  = 0x42
	valueWebsite  = 0x05
	entriesPerPg  = 12
	typesPerPage  = 13
	entries29Last = 34
)

// launcher reports whether a position with this entry and type launches
// something (application, website or third-party application).
func launcher(e proto.Entry29, te proto.TypeEntry) bool {
	switch {
	case e.Kind == proto.KindApp:
		return true
	case te.Type == typeThirdApp:
		return true
	case te.Type == proto.TypeApp && (te.Value == proto.AppType.Value || te.Value == valueWebsite):
		return true
	}
	return false
}

// typeIndex is the 0x25 entry of 0x29 entry i: page g position p sits at
// 12(g-1)+p in 0x29 and 13(g-1)+p in 0x25 (page markers at 12 and 25).
func typeIndex(i int) int { return typesPerPage*(i/entriesPerPg) + i%entriesPerPg }

// ClearLaunchers empties every application, website and third-party
// application button of one profile, on all three pages, so their picture
// slots are no longer shown anywhere. It refuses a profile that kb7screen
// manages (the ledger lists positions of it): use restore for those. The
// positions become empty (an all-zero entry, type 00/00), so the background
// shows there. It returns the cleared positions as "page G position P".
func ClearLaunchers(ctx context.Context, kb Keyboard, l *ledger.Ledger, profile byte) ([]string, error) {
	if profile > maxProfile {
		return nil, fmt.Errorf("profile %d: profiles are 0 to %d", profile, maxProfile)
	}
	if owners := l.Owners(int(profile)); len(owners) > 0 {
		return nil, fmt.Errorf("profile %d holds buttons of this configuration; use restore for it", profile)
	}
	if err := stopped(ctx); err != nil {
		return nil, err
	}
	if err := kb.EnableNotifications(); err != nil {
		return nil, err
	}
	if err := newSession(kb).switchTo(profile); err != nil {
		return nil, err
	}
	t29, err := kb.ReadButtons(profile)
	if err != nil {
		return nil, fmt.Errorf("profile %d: %w", profile, err)
	}
	t25, err := kb.ReadTypes(profile)
	if err != nil {
		return nil, fmt.Errorf("profile %d: %w", profile, err)
	}
	var cleared []string
	for i := 0; i <= entries29Last; i++ {
		ti := typeIndex(i)
		if !launcher(t29.Entry(i), t25.Type(ti)) {
			continue
		}
		if err := t29.SetEntry(i, proto.Entry29{}); err != nil {
			return nil, err
		}
		if err := t25.SetType(ti, proto.TypeEntry{}); err != nil {
			return nil, err
		}
		cleared = append(cleared, fmt.Sprintf("page %d position %d", i/entriesPerPg+1, i%entriesPerPg))
	}
	if len(cleared) == 0 {
		return nil, nil
	}
	if err := kb.WriteButtons(t29); err != nil {
		return nil, err
	}
	if err := writeTypes(kb, t25); err != nil {
		return nil, err
	}
	return cleared, nil
}
