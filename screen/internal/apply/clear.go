// SPDX-License-Identifier: GPL-2.0-or-later

package apply

import (
	"context"
	"fmt"

	"github.com/Bananenbaas/commander/screen/internal/ledger"
	"github.com/Bananenbaas/commander/screen/internal/proto"
)

// Kinds and types of the buttons Swarm II makes for launching things, as
// seen in 40-swarm-slots-pages-1: an application (0x29 kind 0x0b, type
// 05/06), a website (type 05/05; its 0x29 entry starts 07 0b 00) and a
// third-party application (type 0x42).
const (
	typeThirdApp = 0x42
	valueWebsite = 0x05
)

// launcherType reports whether a 0x25 type launches something.
func launcherType(te proto.TypeEntry) bool {
	return te.Type == typeThirdApp ||
		te.Type == proto.TypeApp && (te.Value == proto.AppType.Value || te.Value == valueWebsite)
}

// launcher reports whether a position with this entry and type launches
// something (application, website or third-party application).
func launcher(e proto.Entry29, te proto.TypeEntry) bool {
	return e.Kind == proto.KindApp || launcherType(te)
}

// ClearLaunchers empties every application, website and third-party
// application button of one profile, on all three pages, so their picture
// slots are no longer shown anywhere. The positions the ledger lists are
// kb7screen's own buttons and are left as they are: use restore for those.
// The positions become empty (an all-zero entry, type 00/00), so the
// background shows there. It returns the cleared positions as
// "page G position P".
func ClearLaunchers(ctx context.Context, kb Keyboard, l *ledger.Ledger, profile byte) ([]string, error) {
	if profile > maxProfile {
		return nil, fmt.Errorf("profile %d: profiles are 0 to %d", profile, maxProfile)
	}
	own := map[ledger.Owner]bool{}
	for _, o := range l.Owners(int(profile)) {
		own[o] = true
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
	for _, o := range places(int(profile)) {
		i, ti, err := indexes(o)
		if err != nil {
			return nil, err
		}
		if own[o] || !launcher(t29.Entry(i), t25.Type(ti)) {
			continue
		}
		if err := t29.SetEntry(i, proto.Entry29{}); err != nil {
			return nil, err
		}
		if err := t25.SetType(ti, proto.TypeEntry{}); err != nil {
			return nil, err
		}
		cleared = append(cleared, fmt.Sprintf("page %d position %d", o.Page, o.Position))
	}
	// Page 3 position 11 has a 0x25 entry but no 0x29 entry.
	lastType, err := proto.TypeIndex(proto.Pages, proto.PagePositions-1)
	if err != nil {
		return nil, err
	}
	if launcherType(t25.Type(lastType)) {
		if err := t25.SetType(lastType, proto.TypeEntry{}); err != nil {
			return nil, err
		}
		cleared = append(cleared, "page 3 position 11")
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
