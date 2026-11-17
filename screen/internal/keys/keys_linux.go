// SPDX-License-Identifier: GPL-2.0-or-later

package keys

import "context"

// Listen is not available on Linux yet.
func Listen(ctx context.Context) (<-chan Event, error) {
	return nil, ErrUnsupported
}
