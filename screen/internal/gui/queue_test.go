// SPDX-License-Identifier: GPL-2.0-or-later

package gui

import (
	"fmt"
	"slices"
	"testing"
)

func TestQueueKeepsOrder(t *testing.T) {
	var q lineQueue
	q.push("a", false)
	q.push("b", true)
	q.push("c", false)
	if got := q.take(); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("got %v", got)
	}
	if got := q.take(); len(got) != 0 {
		t.Fatalf("second take got %v", got)
	}
}

func TestQueueDropsOldestEventWhenFull(t *testing.T) {
	var q lineQueue
	q.push("answer0", true)
	for i := 1; i < maxQueue; i++ {
		q.push(fmt.Sprint("event", i), false)
	}
	q.push("answer1", true)    // full: event1 goes
	q.push("event-new", false) // full: event2 goes
	got := q.take()
	if len(got) != maxQueue {
		t.Fatalf("got %d lines, want %d", len(got), maxQueue)
	}
	if got[0] != "answer0" || got[1] != "event3" || got[len(got)-2] != "answer1" || got[len(got)-1] != "event-new" {
		t.Fatalf("got %v ... %v", got[:3], got[len(got)-2:])
	}
}

func TestQueueNeverDropsAnswers(t *testing.T) {
	var q lineQueue
	for i := 0; i < maxQueue+10; i++ {
		q.push(fmt.Sprint("answer", i), true)
	}
	q.push("event", false) // full of answers: the new event is the one dropped
	got := q.take()
	if len(got) != maxQueue+10 || got[0] != "answer0" || got[len(got)-1] != fmt.Sprint("answer", maxQueue+9) {
		t.Fatalf("got %d lines, first %q last %q", len(got), got[0], got[len(got)-1])
	}
}
