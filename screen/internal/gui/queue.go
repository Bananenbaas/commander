// SPDX-License-Identifier: GPL-2.0-or-later

package gui

// maxQueue is the most lines waiting for the window thread before event
// lines are dropped. Answers to calls are never dropped: the page waits for
// each one.
const maxQueue = 1024

// queued is one line for the page.
type queued struct {
	line   string
	answer bool // an answer to a call, never dropped
}

// lineQueue holds the lines waiting for the window thread. It is not safe
// for concurrent use; the window guards it with its lock.
type lineQueue struct {
	items []queued
}

// push adds a line. When the queue is full, the oldest event line makes
// room; if it holds answers only, a new event line is dropped instead.
func (q *lineQueue) push(line string, answer bool) {
	if len(q.items) >= maxQueue {
		i := 0
		for i < len(q.items) && q.items[i].answer {
			i++
		}
		switch {
		case i < len(q.items):
			q.items = append(q.items[:i], q.items[i+1:]...)
		case !answer:
			return
		}
	}
	q.items = append(q.items, queued{line, answer})
}

// take returns the queued lines in order and empties the queue.
func (q *lineQueue) take() []string {
	lines := make([]string, len(q.items))
	for i, it := range q.items {
		lines[i] = it.line
	}
	q.items = nil
	return lines
}
