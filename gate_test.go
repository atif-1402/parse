// Tests for the gate that decides whether piped output is a flood. The gate
// is set up by hand here rather than through runPipe, because there is no
// terminal to measure in a test.
package main

import (
	"bufio"
	"bytes"
	"testing"
)

// Output that fits is held back until the stream says how much there is, then
// printed without ever opening a pager. Output that does not fit opens one and
// releases everything held, so nothing that was printed early is missing from
// what the reader gets.
func TestGatePagesOnlyFloods(t *testing.T) {
	var dst bytes.Buffer
	started := false
	g := &screenGate{
		to:    bufio.NewWriter(&dst),
		limit: 3,
		width: 80,
		start: func() { started = true },
	}

	for _, line := range []string{"one\n", "two\n"} {
		if _, err := g.Write([]byte(line)); err != nil {
			t.Fatalf("write %q: %v", line, err)
		}
	}
	if started {
		t.Errorf("two rows were paged")
	}
	if dst.Len() != 0 {
		t.Errorf("wrote %q before the answer was known", dst.String())
	}

	g.Write([]byte("three\n"))
	if started {
		t.Errorf("exactly one screenful was paged")
	}

	g.Write([]byte("four\n"))
	if !started {
		t.Errorf("a fourth row did not open a pager")
	}
	g.to.Flush()
	if got, want := dst.String(), "one\ntwo\nthree\nfour\n"; got != want {
		t.Errorf("released %q, want %q", got, want)
	}

	// Once the answer is known nothing is held back any more.
	g.Write([]byte("five\n"))
	g.to.Flush()
	if got, want := dst.String(), "one\ntwo\nthree\nfour\nfive\n"; got != want {
		t.Errorf("after the pager opened: %q, want %q", got, want)
	}
}

// A screenful is counted in rows, not bytes: a line that runs past the
// terminal's width wraps and takes more than one row, so a single long line is
// a flood where a hundred bytes would not be.
func TestGateCountsWrappedLines(t *testing.T) {
	var dst bytes.Buffer
	started := false
	g := &screenGate{
		to:    bufio.NewWriter(&dst),
		limit: 2,
		width: 10,
		start: func() { started = true },
	}

	// Twenty-five characters at ten columns wraps to three rows, which is one
	// row more than the two this terminal has.
	g.Write([]byte("1234567890123456789012345\n"))
	if !started {
		t.Errorf("a line eight rows tall was printed straight through")
	}
	g.to.Flush()
	if dst.Len() == 0 {
		t.Errorf("the flood was paged but never written out")
	}
}

// A gate with nowhere to page to never holds anything: redirected output must
// arrive as it arrives, with no buffer in the way.
func TestGateWithNoPagerHoldsNothing(t *testing.T) {
	var dst bytes.Buffer
	g := &screenGate{to: bufio.NewWriter(&dst)}
	g.release()

	g.Write([]byte("one\ntwo\n"))
	g.to.Flush()
	if got, want := dst.String(), "one\ntwo\n"; got != want {
		t.Errorf("wrote %q, want %q", got, want)
	}
}
