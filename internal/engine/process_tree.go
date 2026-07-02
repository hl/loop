package engine

// orderTreePIDs returns root and all of its descendants, per the parent→children
// map, in post-order (children before their parent). A visited set guarantees
// each PID is emitted at most once, so parent-PID cycles — possible on Windows
// because PIDs are reused and ParentProcessID is recorded at creation time — and
// self-loops (pid == parent) cannot cause infinite recursion.
func orderTreePIDs(children map[uint32][]uint32, root uint32) []uint32 {
	visited := make(map[uint32]bool)
	var order []uint32
	var walk func(uint32)
	walk = func(pid uint32) {
		if visited[pid] {
			return
		}
		visited[pid] = true
		for _, child := range children[pid] {
			walk(child)
		}
		order = append(order, pid)
	}
	walk(root)
	return order
}
