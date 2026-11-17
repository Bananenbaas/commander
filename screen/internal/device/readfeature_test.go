// SPDX-License-Identifier: GPL-2.0-or-later

package device

import (
	"errors"
	"testing"
)

func TestReadFeatureRetry(t *testing.T) {
	fail := errors.New("error 31")
	t.Run("first read works", func(t *testing.T) {
		var lens []int
		got, err := readFeature(5, 4, 1044, func(b []byte) error { lens = append(lens, len(b)); return nil })
		if err != nil || len(got) != 4 || got[0] != 5 || len(lens) != 1 {
			t.Fatalf("got %v %v lens %v", got, err, lens)
		}
	})
	t.Run("short read retried with 64", func(t *testing.T) {
		var lens []int
		got, err := readFeature(5, 4, 1044, func(b []byte) error {
			lens = append(lens, len(b))
			if len(b) < 64 {
				return fail
			}
			copy(b, []byte{5, 1, 2, 3, 4})
			return nil
		})
		if err != nil || len(lens) != 2 || lens[1] != 64 || len(got) != 4 || got[1] != 1 || got[3] != 3 {
			t.Fatalf("got %v %v lens %v", got, err, lens)
		}
	})
	t.Run("long report retried with 1044", func(t *testing.T) {
		var lens []int
		_, err := readFeature(5, 100, 1044, func(b []byte) error {
			lens = append(lens, len(b))
			if len(b) < 1044 {
				return fail
			}
			return nil
		})
		if err != nil || len(lens) != 2 || lens[1] != 1044 {
			t.Fatalf("err %v lens %v", err, lens)
		}
	})
	t.Run("disconnect is not retried", func(t *testing.T) {
		calls := 0
		_, err := readFeature(5, 4, 1044, func(b []byte) error { calls++; return disconnected("get", nil) })
		if !errors.Is(err, ErrDisconnected) || calls != 1 {
			t.Fatalf("err %v calls %d", err, calls)
		}
	})
	t.Run("both fail returns the first error after one retry", func(t *testing.T) {
		calls := 0
		_, err := readFeature(5, 4, 1044, func(b []byte) error { calls++; return fail })
		if err != fail || calls != 2 {
			t.Fatalf("err %v calls %d", err, calls)
		}
	})
	t.Run("no retry beyond the device limit", func(t *testing.T) {
		calls := 0
		_, err := readFeature(5, 4, 32, func(b []byte) error { calls++; return fail })
		if err != fail || calls != 1 {
			t.Fatalf("err %v calls %d", err, calls)
		}
	})
}
