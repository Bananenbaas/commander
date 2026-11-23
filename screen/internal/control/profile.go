// SPDX-License-Identifier: GPL-2.0-or-later

package control

import (
	"encoding/json"
	"fmt"

	"github.com/Bananenbaas/commander/screen/internal/layout"
	"github.com/Bananenbaas/commander/screen/internal/store"
)

// profileChangedText answers a change of a profile changed on disk.
const profileChangedText = "This profile was changed outside Commander since you opened it."

// deleteLayout is store.DeleteProfile; tests replace it.
var deleteLayout = store.DeleteProfile

// profileSlot checks the profile of a call: given, and 0 to 4.
func profileSlot(call string, profile *int) (byte, error) {
	if profile == nil || *profile < 0 || *profile > 4 {
		return 0, fmt.Errorf("%s: no profile 0 to 4 given", call)
	}
	return byte(*profile), nil
}

type profileInfoOut struct {
	Profile      int      `json:"profile"`
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	UsedBy       []string `json:"usedBy"`
	CanDuplicate bool     `json:"canDuplicate"`
}

func (s *Service) profileInfo(args json.RawMessage) (any, error) {
	var a struct {
		Profile *int `json:"profile"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("profileInfo: bad arguments: %w", err)
	}
	slot, err := profileSlot("profileInfo", a.Profile)
	if err != nil {
		return nil, err
	}
	data, f, err := layoutOf(s.d.Dir, slot)
	if err != nil {
		return nil, fmt.Errorf("profileInfo: %w", err)
	}
	cfg, err := s.d.Load(s.d.Dir)
	if err != nil {
		return nil, fmt.Errorf("profileInfo: %w", err)
	}
	out := profileInfoOut{
		Profile: int(slot), Version: store.Version(data),
		UsedBy: usedBy(cfg, slot), CanDuplicate: len(cfg.Profiles) < 5,
	}
	if f.Name != nil {
		out.Name = *f.Name
	}
	return out, nil
}

// usedBy names the game profiles, Desktop first, that switch the KB7 to
// slot.
func usedBy(cfg *layout.Config, slot byte) []string {
	names := []string{}
	if cfg.DesktopProfile() == slot {
		names = append(names, "Desktop")
	}
	for _, g := range cfg.Games {
		if g.KB7 != nil && *g.KB7 == slot {
			names = append(names, g.Name)
		}
	}
	return names
}

func (s *Service) saveProfileName(args json.RawMessage) (any, error) {
	var a struct {
		Profile *int   `json:"profile"`
		Version string `json:"version"`
		Name    string `json:"name"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("saveProfileName: bad arguments: %w", err)
	}
	slot, err := profileSlot("saveProfileName", a.Profile)
	if err != nil {
		return nil, err
	}
	res, err := store.SaveProfileName(s.d.Dir, slot, a.Version, a.Name)
	return s.saveAnswer("saveProfileName", profileChangedText, res, err, noRefusals), nil
}

func (s *Service) duplicateProfile(args json.RawMessage) (any, error) {
	var a struct {
		Profile *int `json:"profile"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("duplicateProfile: bad arguments: %w", err)
	}
	slot, err := profileSlot("duplicateProfile", a.Profile)
	if err != nil {
		return nil, err
	}
	n, res, err := store.DuplicateProfile(s.d.Dir, slot)
	out := s.saveAnswer("duplicateProfile", profileChangedText, res, err, noRefusals)
	if out["ok"] == true {
		out["newProfile"] = int(n)
	}
	return out, nil
}

func (s *Service) deleteProfile(args json.RawMessage) (any, error) {
	var a struct {
		Profile *int   `json:"profile"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("deleteProfile: bad arguments: %w", err)
	}
	slot, err := profileSlot("deleteProfile", a.Profile)
	if err != nil {
		return nil, err
	}
	res, err := deleteLayout(s.d.Dir, slot, a.Version)
	return s.saveAnswer("deleteProfile", profileChangedText, res, err, noRefusals), nil
}
