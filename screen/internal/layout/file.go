// SPDX-License-Identifier: GPL-2.0-or-later

package layout

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
)

// Header starts every file Commander writes.
const Header = "# Saved by Commander. Commander rewrites this file when you save in its window; comments are not kept.\n"

// File is one layout file as it is written: every key the format has.
type File struct {
	Profile           int          `toml:"profile"`
	Name              *string      `toml:"name,omitempty"`
	SwitchWhenRunning string       `toml:"switch_when_running,omitempty"`
	Light             *FileLight   `toml:"light,omitempty"`
	StoredLight       *FileStored  `toml:"stored_light,omitempty"`
	Buttons           []FileButton `toml:"button,omitempty"`
}

// FileLight is the [light] table.
type FileLight struct {
	FPS    int64            `toml:"fps,omitempty"`
	Layers []map[string]any `toml:"layer,omitempty"`
}

// FileStored is the [stored_light] table.
type FileStored struct {
	Color      string `toml:"color,omitempty"`
	Brightness *int64 `toml:"brightness,omitempty"`
}

// FileButton is one [[button]] table.
type FileButton struct {
	Page     int      `toml:"page"`
	Position int      `toml:"position"`
	Title    string   `toml:"title,omitempty"`
	Label    string   `toml:"label,omitempty"`
	Color    string   `toml:"color,omitempty"`
	Symbol   string   `toml:"symbol,omitempty"`  // relative to the folder, slashes
	Picture  string   `toml:"picture,omitempty"` // relative to the folder, slashes
	Action   []string `toml:"action,omitempty"`
	Mode     string   `toml:"mode,omitempty"`
	State    string   `toml:"state,omitempty"`
	Confirm  string   `toml:"confirm,omitempty"`
}

// GameFile is one file of games/ as it is written.
type GameFile struct {
	Name    string         `toml:"name,omitempty"`
	Program string         `toml:"program,omitempty"`
	Devices map[string]any `toml:"devices"` // "kb7": int64 profile 0-4
}

// DecodeFile reads a layout file's bytes. Unknown keys are an error, as in Load.
func DecodeFile(data []byte) (*File, error) {
	var f File
	if err := decodeStrict(data, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// DecodeGameFile reads a games/ file's bytes. Unknown keys are an error.
func DecodeGameFile(data []byte) (*GameFile, error) {
	var g GameFile
	if err := decodeStrict(data, &g); err != nil {
		return nil, err
	}
	return &g, nil
}

func decodeStrict(data []byte, v any) error {
	md, err := toml.Decode(string(data), v)
	if err != nil {
		return err
	}
	if un := md.Undecoded(); len(un) > 0 {
		keys := make([]string, len(un))
		for i, k := range un {
			keys[i] = k.String()
		}
		return fmt.Errorf("unknown key(s): %s", strings.Join(keys, ", "))
	}
	return nil
}

// Encode returns the file as TOML: Header, a blank line, then the tables.
func (f *File) Encode() ([]byte, error) { return encode(f) }

// Encode returns the file as TOML: Header, a blank line, then the tables.
func (g *GameFile) Encode() ([]byte, error) { return encode(g) }

func encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(Header + "\n")
	enc := toml.NewEncoder(&buf)
	enc.Indent = ""
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
