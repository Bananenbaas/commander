// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "commander: Commander needs Windows; on this system use kb7screen run, stop and reload")
	os.Exit(1)
}
