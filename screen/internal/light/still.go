// SPDX-License-Identifier: GPL-2.0-or-later

package light

import "github.com/Bananenbaas/commander/screen/internal/proto"

// Still renders s as a still picture for the keyboard's stored lighting: at
// t = 0, without presses and with every state at its start value, not scaled
// by a brightness (the stored table holds that apart). Ripples therefore do
// not show, and waves and breathing are at their starting point. It is nil
// when s is nil.
func Still(s *Stack) *proto.LightPayload {
	if s == nil {
		return nil
	}
	states := map[string]bool{}
	for _, d := range s.states {
		states[d.Name] = d.Start
	}
	return renderPayload(s, 0, &Input{States: states}, func(v uint8) byte { return v })
}
