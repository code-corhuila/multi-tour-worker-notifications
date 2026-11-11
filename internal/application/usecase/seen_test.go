package usecase

import "testing"

// Piece plan D-03: a bounded set; the oldest id leaves first.
func TestA_the_set_of_recent_ids_is_bounded(t *testing.T) {
	s := newSeen(2)
	s.add("a")
	s.add("b")
	s.add("c")
	if s.has("a") {
		t.Fatal("the oldest id should have left the set")
	}
	if !s.has("b") || !s.has("c") {
		t.Fatal("the two newest ids should be in the set")
	}
	s.add("c") // adding a known id must not push another one out
	if !s.has("b") {
		t.Fatal("a repeated add evicted an id")
	}
}
