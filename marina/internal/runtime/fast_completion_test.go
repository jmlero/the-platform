package runtime

import "testing"

func TestFastJobCanFinishBeforeFirstObservation(t *testing.T) {
	for _, state := range []State{StatePending, StateStarting} {
		if !state.CanObserve(StateSucceeded) {
			t.Fatalf("%s must accept an already completed Job", state)
		}
	}
}
