// Package procs reports whether named programs are currently running.
package procs

import "strings"

// Swarm lists the vendor software processes that must not run alongside
// kb7screen.
var Swarm = []string{"Turtle Beach Swarm II.exe", "Turtle Beach Device Service.exe"}

// commLen is the longest process name the Linux kernel keeps in comm.
const commLen = 15

// Running reports, for every wanted name, whether a process with that name is
// running. Names are compared case-insensitively and ".exe" is optional.
func Running(names []string) (map[string]bool, error) {
	running, err := list()
	if err != nil {
		return nil, err
	}
	return match(running, names), nil
}

// match returns one entry per wanted name, keyed exactly as given.
func match(running, wanted []string) map[string]bool {
	norm := make(map[string]bool, len(running))
	raw := make(map[string]bool, len(running))
	for _, r := range running {
		r = strings.ToLower(base(r))
		raw[r] = true
		norm[strings.TrimSuffix(r, ".exe")] = true
	}
	out := make(map[string]bool, len(wanted))
	for _, w := range wanted {
		lw := strings.ToLower(base(w))
		found := norm[strings.TrimSuffix(lw, ".exe")]
		if !found && len(lw) > commLen {
			// A Linux comm is cut to commLen bytes of the full name.
			found = raw[lw[:commLen]]
		}
		out[w] = found
	}
	return out
}

// base strips leading directory parts, accepting both separators.
func base(s string) string {
	if i := strings.LastIndexAny(s, `/\`); i >= 0 {
		return s[i+1:]
	}
	return s
}
