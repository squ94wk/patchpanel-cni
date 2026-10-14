package agent_test

import (
	"slices"
	"testing"

	"github.com/squ94wk/patchpanel-cni/internal/agent"
)

func TestBrokenStubs(t *testing.T) {
	tests := []struct {
		name  string
		stubs []agent.StubTarget
		want  []int
	}{
		{
			name:  "wired pair",
			stubs: []agent.StubTarget{{Index: 1, Up: true, Target: 2}, {Index: 2, Up: true, Target: 1}},
		},
		{
			name:  "up with target 0",
			stubs: []agent.StubTarget{{Index: 1, Up: true, Target: 0}},
			want:  []int{1},
		},
		{
			name:  "target missing",
			stubs: []agent.StubTarget{{Index: 1, Up: true, Target: 9}},
			want:  []int{1},
		},
		{
			name:  "target does not point back",
			stubs: []agent.StubTarget{{Index: 1, Up: true, Target: 2}, {Index: 2, Up: true, Target: 3}, {Index: 3, Up: true, Target: 2}},
			want:  []int{1},
		},
		{
			name:  "down stubs are ignored",
			stubs: []agent.StubTarget{{Index: 1, Up: false, Target: 0}, {Index: 2, Up: false, Target: 9}},
		},
		{
			name:  "up stub with a down peer that points back",
			stubs: []agent.StubTarget{{Index: 1, Up: true, Target: 2}, {Index: 2, Up: false, Target: 1}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := agent.BrokenStubs(tt.stubs)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
