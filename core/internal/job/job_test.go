package job

import "testing"

func TestTransitions(t *testing.T) {
	allowed := map[[2]State]bool{
		{StateQueued, StateRunning}:  true,
		{StateQueued, StateFailed}:   true,
		{StateRunning, StateDone}:    true,
		{StateRunning, StateFailed}:  true,
		{StateRunning, StateBlocked}: true,
	}
	states := []State{StateQueued, StateRunning, StateDone, StateFailed, StateBlocked}
	for _, from := range states {
		for _, to := range states {
			want := allowed[[2]State{from, to}]
			if got := CanTransition(from, to); got != want {
				t.Errorf("CanTransition(%s, %s) = %v, want %v", from, to, got, want)
			}
		}
	}
}

func TestTerminal(t *testing.T) {
	for state, want := range map[State]bool{
		StateQueued: false, StateRunning: false,
		StateDone: true, StateFailed: true, StateBlocked: true,
	} {
		if got := state.Terminal(); got != want {
			t.Errorf("%s.Terminal() = %v, want %v", state, got, want)
		}
	}
}

func TestBrokerSubscribePublish(t *testing.T) {
	b := NewBroker()
	ch, cancel := b.Subscribe("j1")
	defer cancel()

	b.Publish("j1", Event{Type: EventProgress, Step: 3, Total: 24})
	b.Publish("other", Event{Type: EventDone})

	select {
	case ev := <-ch:
		if ev.Type != EventProgress || ev.Step != 3 {
			t.Errorf("got %+v", ev)
		}
	default:
		t.Fatal("no event delivered")
	}
	select {
	case ev := <-ch:
		t.Fatalf("cross-job event leaked: %+v", ev)
	default:
	}
}

func TestBrokerNeverBlocks(t *testing.T) {
	b := NewBroker()
	_, cancel := b.Subscribe("j1")
	defer cancel()
	for i := 0; i < 100; i++ { // over channel capacity — must drop, not hang
		b.Publish("j1", Event{Type: EventProgress, Step: i})
	}
}
