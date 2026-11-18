// SPDX-License-Identifier: GPL-2.0-or-later

package update

import "testing"

func TestCompareOrder(t *testing.T) {
	// ascending
	tags := []string{"v0.1.0-alpha.1", "v0.1.0-beta.1", "v0.1.0-beta.2", "v0.1.0-beta.9",
		"v0.1.0-beta.10", "v0.1.0-rc.1", "v0.1.0", "v0.1.1", "v0.2.0", "v1.0.0"}
	vs := make([]Version, len(tags))
	for i, s := range tags {
		v, err := ParseVersion(s)
		if err != nil {
			t.Fatalf("ParseVersion(%q): %v", s, err)
		}
		if v.String() != s {
			t.Errorf("String() = %q, want %q", v.String(), s)
		}
		vs[i] = v
	}
	for i := range vs {
		if c := vs[i].Compare(vs[i]); c != 0 {
			t.Errorf("%s vs itself = %d", tags[i], c)
		}
		for j := i + 1; j < len(vs); j++ {
			if c := vs[i].Compare(vs[j]); c != -1 {
				t.Errorf("%s vs %s = %d, want -1", tags[i], tags[j], c)
			}
			if c := vs[j].Compare(vs[i]); c != 1 {
				t.Errorf("%s vs %s = %d, want 1", tags[j], tags[i], c)
			}
		}
	}
}

func TestComparePrefixAndNumeric(t *testing.T) {
	// A shorter list ranks below a longer one with an equal prefix; a numeric
	// identifier ranks below an alphanumeric one.
	order := []string{"v1.0.0-1", "v1.0.0-a", "v1.0.0-a.1", "v1.0.0-a.b"}
	for i := 0; i+1 < len(order); i++ {
		a, _ := ParseVersion(order[i])
		b, _ := ParseVersion(order[i+1])
		if a.Compare(b) != -1 {
			t.Errorf("%s should rank below %s", order[i], order[i+1])
		}
	}
}

func TestParseVersionRejects(t *testing.T) {
	for _, s := range []string{"", "dev", "0.1.0", "v1.0", "v1.0.0+build.1", "v1.0.0-", "v01.0.0", "v1.0.0-beta..1", "v1.x.0",
		"v1.0.0-01", "v1.0.0-be ta", "v-1.0.0", "v1.0.0.0", "v99999999999999999999.0.0"} {
		if v, err := ParseVersion(s); err == nil {
			t.Errorf("ParseVersion(%q) = %v, want an error", s, v)
		}
	}
}
