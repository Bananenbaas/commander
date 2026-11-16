package action

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Step is one element of a button's action: Chord, Wait, Run or Open.
type Step interface{}

// Chord presses all keys together and releases them again.
type Chord struct{ Keys []Key }

// Wait pauses between steps.
type Wait struct{ D time.Duration }

// Run starts a program.
type Run struct {
	Program string
	Args    []string
}

// Open opens a URL, file or folder with its default handler.
type Open struct{ Target string }

const maxWait = 60000

// ParseStep parses one action step string.
func ParseStep(s string) (Step, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty step")
	}
	word, rest := s, ""
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		word, rest = s[:i], strings.TrimSpace(s[i+1:])
	}
	switch strings.ToLower(word) {
	case "wait":
		return parseWait(rest)
	case "run":
		return parseRun(rest)
	case "open":
		if rest == "" {
			return nil, fmt.Errorf("open needs a target")
		}
		return Open{Target: rest}, nil
	}
	return parseChord(s)
}

func parseWait(arg string) (Step, error) {
	num, ok := strings.CutSuffix(strings.ToLower(arg), "ms")
	if !ok {
		return nil, fmt.Errorf("wait needs a duration like \"wait 600ms\", got %q", arg)
	}
	n, err := strconv.Atoi(strings.TrimSpace(num))
	if err != nil || n <= 0 || n > maxWait {
		return nil, fmt.Errorf("wait must be a whole number of milliseconds from 1 to %d, got %q", maxWait, arg)
	}
	return Wait{D: time.Duration(n) * time.Millisecond}, nil
}

func parseRun(arg string) (Step, error) {
	fields, err := splitArgs(arg)
	if err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("run needs a program")
	}
	r := Run{Program: fields[0]}
	if len(fields) > 1 {
		r.Args = fields[1:]
	}
	return r, nil
}

// splitArgs splits on spaces; double quotes group words and are removed.
func splitArgs(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	inQuote, have := false, false
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
			have = true
		case (r == ' ' || r == '\t') && !inQuote:
			if have {
				out = append(out, cur.String())
				cur.Reset()
				have = false
			}
		default:
			cur.WriteRune(r)
			have = true
		}
	}
	if inQuote {
		return nil, fmt.Errorf("unterminated double quote in %q", s)
	}
	if have {
		out = append(out, cur.String())
	}
	return out, nil
}

func parseChord(s string) (Step, error) {
	var keys []Key
	seen := map[Key]bool{}
	add := func(k Key, name string) error {
		if seen[k] {
			return fmt.Errorf("key %q repeats in chord %q", name, s)
		}
		seen[k] = true
		keys = append(keys, k)
		return nil
	}
	for _, part := range strings.Split(s, "+") {
		name := strings.TrimSpace(part)
		if strings.EqualFold(name, "AltGr") {
			if err := add(LCtrl, name); err != nil {
				return nil, err
			}
			if err := add(RAlt, name); err != nil {
				return nil, err
			}
			continue
		}
		k, err := ParseKey(name)
		if err != nil {
			return nil, err
		}
		if err := add(k, name); err != nil {
			return nil, err
		}
	}
	return Chord{Keys: keys}, nil
}
