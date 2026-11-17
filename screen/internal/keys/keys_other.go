// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows && !linux

package keys

import "context"

// Listen is not available on this system.
func Listen(ctx context.Context) (<-chan Event, error) {
	return nil, ErrUnsupported
}
