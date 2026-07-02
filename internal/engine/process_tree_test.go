package engine

import (
	"sort"
	"testing"
)

func TestOrderTreePIDsSimpleTree(t *testing.T) {
	// 1 → {2, 3}, 2 → {4}
	children := map[uint32][]uint32{
		1: {2, 3},
		2: {4},
	}
	got := orderTreePIDs(children, 1)

	// Every node reachable from the root must appear exactly once.
	want := []uint32{1, 2, 3, 4}
	if !sameSet(got, want) {
		t.Fatalf("got %v, want the set %v", got, want)
	}
	// Post-order: each child precedes its parent.
	if idx(got, 4) > idx(got, 2) || idx(got, 2) > idx(got, 1) || idx(got, 3) > idx(got, 1) {
		t.Fatalf("expected post-order (children before parents), got %v", got)
	}
}

func TestOrderTreePIDsCycleTerminates(t *testing.T) {
	// A parent→child cycle: 1 → 2 → 3 → 1. Must terminate and emit each PID once.
	children := map[uint32][]uint32{
		1: {2},
		2: {3},
		3: {1},
	}
	got := orderTreePIDs(children, 1)
	if !sameSet(got, []uint32{1, 2, 3}) {
		t.Fatalf("cycle produced wrong set: %v", got)
	}
}

func TestOrderTreePIDsSelfLoop(t *testing.T) {
	// pid == parent self-loop must not recurse forever.
	children := map[uint32][]uint32{
		1: {1, 2},
	}
	got := orderTreePIDs(children, 1)
	if !sameSet(got, []uint32{1, 2}) {
		t.Fatalf("self-loop produced wrong set: %v", got)
	}
}

func sameSet(a, b []uint32) bool {
	if len(a) != len(b) {
		return false
	}
	ac := append([]uint32(nil), a...)
	bc := append([]uint32(nil), b...)
	sort.Slice(ac, func(i, j int) bool { return ac[i] < ac[j] })
	sort.Slice(bc, func(i, j int) bool { return bc[i] < bc[j] })
	for i := range ac {
		if ac[i] != bc[i] {
			return false
		}
	}
	return true
}

func idx(s []uint32, v uint32) int {
	for i := range s {
		if s[i] == v {
			return i
		}
	}
	return -1
}
