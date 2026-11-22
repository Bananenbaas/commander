// SPDX-License-Identifier: GPL-2.0-or-later

//go:build ignore

// Command gen writes the Commander icon files. Run it in the screen module:
//
//	go run internal/icon/gen/main.go
//
// It writes cmd/commander/commander.ico, cmd/commander/rsrc_windows_amd64.syso
// (the icon and the application manifest) and internal/icon/helmet-256.png.
package main

import (
	"bytes"
	"image/png"
	"log"
	"os"
	"path/filepath"

	"github.com/Bananenbaas/commander/screen/internal/icon"
)

func main() {
	ico, err := icon.ICO(icon.Sizes, false)
	if err != nil {
		log.Fatal(err)
	}
	syso, err := icon.Syso(ico, []byte(icon.Manifest))
	if err != nil {
		log.Fatal(err)
	}
	var ref bytes.Buffer
	if err := png.Encode(&ref, icon.Draw(256, false)); err != nil {
		log.Fatal(err)
	}
	dir := filepath.Join("cmd", "commander")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatal(err)
	}
	files := map[string][]byte{
		filepath.Join(dir, "commander.ico"):                 ico,
		filepath.Join(dir, "rsrc_windows_amd64.syso"):       syso,
		filepath.Join("internal", "icon", "helmet-256.png"): ref.Bytes(),
	}
	for path, data := range files {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			log.Fatal(err)
		}
	}
}
