// SPDX-License-Identifier: GPL-2.0-or-later

package update

import (
	"encoding/hex"
	"strings"
	"testing"
)

func rel(tag string, draft bool, assets ...string) Release {
	r := Release{Tag: tag, Draft: draft}
	for _, a := range assets {
		r.Assets = append(r.Assets, Asset{Name: a, URL: "https://example.invalid/" + tag + "/" + a, Size: 1})
	}
	return r
}

func TestPick(t *testing.T) {
	both := []string{ExeAsset, SumsAsset}
	releases := []Release{
		rel("v0.3.0", true, both...),
		rel("v0.2.0", false, ExeAsset),
		rel("nightly", false, both...),
		rel("v0.1.0-beta.2", false, both...),
		rel("v0.1.0-beta.1", false, both...),
	}
	got, ok, err := Pick("v0.1.0-beta.1", releases)
	if err != nil || !ok || got.Tag != "v0.1.0-beta.2" {
		t.Errorf("Pick(beta.1) = %q, %v, %v", got.Tag, ok, err)
	}
	if _, ok, err := Pick("v0.1.0-beta.2", releases); err != nil || ok {
		t.Errorf("Pick(beta.2) ok=%v err=%v, want no update", ok, err)
	}
	if _, ok, err := Pick("v9.0.0", releases); err != nil || ok {
		t.Errorf("Pick(v9.0.0) ok=%v err=%v, want no update", ok, err)
	}
	if _, _, err := Pick("dev", releases); err == nil {
		t.Error("Pick(dev) want an error")
	}
}

func TestPickNewestRegardlessOfOrder(t *testing.T) {
	both := []string{ExeAsset, SumsAsset}
	releases := []Release{rel("v0.1.1", false, both...), rel("v0.2.0", false, both...), rel("v0.1.5", false, both...)}
	got, ok, err := Pick("v0.1.0", releases)
	if err != nil || !ok || got.Tag != "v0.2.0" {
		t.Errorf("Pick = %q, %v, %v", got.Tag, ok, err)
	}
}

func TestAsset(t *testing.T) {
	r := rel("v1.0.0", false, ExeAsset)
	if a, ok := r.Asset(ExeAsset); !ok || a.Name != ExeAsset {
		t.Errorf("Asset(exe) = %v, %v", a, ok)
	}
	if _, ok := r.Asset(SumsAsset); ok {
		t.Error("Asset(sums) found")
	}
}

func TestSumFor(t *testing.T) {
	const a = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	const b = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
	sums := []byte(a + "  kb7screen-windows-amd64.exe\n" + strings.ToUpper(b) + "  *commander-windows-amd64.exe\n")
	got, err := SumFor(sums, "commander-windows-amd64.exe")
	if err != nil {
		t.Fatal(err)
	}
	want, _ := hex.DecodeString(b)
	if string(got[:]) != string(want) {
		t.Errorf("sum = %x, want %x", got, want)
	}
	if got, err := SumFor(sums, "kb7screen-windows-amd64.exe"); err != nil || hex.EncodeToString(got[:]) != a {
		t.Errorf("first line: %x, %v", got, err)
	}
	if _, err := SumFor(sums, "missing.exe"); err == nil {
		t.Error("missing name: want an error")
	}
	short := []byte(a[:63] + "  commander-windows-amd64.exe\r\n")
	if _, err := SumFor(short, "commander-windows-amd64.exe"); err == nil {
		t.Error("63 hex chars: want an error")
	}
	bad := []byte(strings.Repeat("g", 64) + "  commander-windows-amd64.exe\n")
	if _, err := SumFor(bad, "commander-windows-amd64.exe"); err == nil {
		t.Error("non-hex: want an error")
	}
	crlf := []byte(a + "  commander-windows-amd64.exe\r\n")
	if _, err := SumFor(crlf, "commander-windows-amd64.exe"); err != nil {
		t.Errorf("CRLF line: %v", err)
	}
}
