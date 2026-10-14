package agent

// StubTarget is a stub in the park netns and the ifindex its ingress mirred action redirects to.
type StubTarget struct {
	Index  int
	Up     bool
	Target int
}

// BrokenStubs returns the indexes of up stubs that do not redirect to an existing stub that redirects back.
// The kernel reports target 0 once the target interface is unregistered.
func BrokenStubs(stubs []StubTarget) []int {
	targets := make(map[int]int, len(stubs))
	for _, s := range stubs {
		targets[s.Index] = s.Target
	}

	var broken []int
	for _, s := range stubs {
		if !s.Up {
			continue
		}

		back, ok := targets[s.Target]
		if s.Target == 0 || !ok || back != s.Index {
			broken = append(broken, s.Index)
		}
	}

	return broken
}
