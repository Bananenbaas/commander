// SPDX-License-Identifier: GPL-2.0-or-later

// Package update finds a newer Commander release on GitHub, checks the files
// it downloads against the published checksums and installs them over the
// running program.
package update

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a release version: v, MAJOR.MINOR.PATCH and optional
// dot-separated pre-release identifiers.
type Version struct {
	Major, Minor, Patch int
	Pre                 []string
}

// ParseVersion reads a version such as v1.2.3 or v1.2.3-beta.4. The v prefix
// is required; fewer parts, build metadata (+...), numbers with a leading zero
// or anything else is not a version.
func ParseVersion(s string) (Version, error) {
	bad := func() (Version, error) {
		return Version{}, fmt.Errorf("%q is not a version like v1.2.3 or v1.2.3-beta.4", s)
	}
	rest, ok := strings.CutPrefix(s, "v")
	if !ok {
		return bad()
	}
	core, pre, hasPre := strings.Cut(rest, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return bad()
	}
	var n [3]int
	for i, p := range parts {
		x, ok := parseNumber(p)
		if !ok {
			return bad()
		}
		n[i] = x
	}
	v := Version{Major: n[0], Minor: n[1], Patch: n[2]}
	if hasPre {
		for _, id := range strings.Split(pre, ".") {
			if !validIdentifier(id) {
				return bad()
			}
			v.Pre = append(v.Pre, id)
		}
	}
	return v, nil
}

// parseNumber reads a non-negative decimal number without a leading zero.
func parseNumber(s string) (int, bool) {
	if s == "" || !isDigits(s) || (len(s) > 1 && s[0] == '0') {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

// validIdentifier reports whether s is a pre-release identifier: letters,
// digits and hyphens, and a number only without a leading zero.
func validIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-') {
			return false
		}
	}
	if isDigits(s) {
		_, ok := parseNumber(s)
		return ok
	}
	return true
}

// Compare orders two versions by semantic-versioning precedence and returns
// -1, 0 or +1 when v is below, equal to or above w.
func (v Version) Compare(w Version) int {
	for _, c := range [3]int{cmpInt(v.Major, w.Major), cmpInt(v.Minor, w.Minor), cmpInt(v.Patch, w.Patch)} {
		if c != 0 {
			return c
		}
	}
	// A release ranks above any pre-release of the same numbers.
	switch {
	case len(v.Pre) == 0 && len(w.Pre) == 0:
		return 0
	case len(v.Pre) == 0:
		return 1
	case len(w.Pre) == 0:
		return -1
	}
	for i := 0; i < len(v.Pre) && i < len(w.Pre); i++ {
		if c := cmpIdentifier(v.Pre[i], w.Pre[i]); c != 0 {
			return c
		}
	}
	return cmpInt(len(v.Pre), len(w.Pre))
}

// cmpIdentifier compares two pre-release identifiers: numbers numerically and
// below text, text byte by byte.
func cmpIdentifier(a, b string) int {
	an, bn := isDigits(a), isDigits(b)
	switch {
	case an && bn:
		// Equal-length digit strings without leading zeros compare as text;
		// a longer one is the larger number, which also avoids overflow.
		if c := cmpInt(len(a), len(b)); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	case an:
		return -1
	case bn:
		return 1
	}
	return strings.Compare(a, b)
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// String formats the version the way it is tagged, for example v1.2.3-beta.4.
func (v Version) String() string {
	s := fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
	if len(v.Pre) > 0 {
		s += "-" + strings.Join(v.Pre, ".")
	}
	return s
}
