package job

import "sync"

// Terminal events carry no payload: subscribers re-read the row.
const (
	EventProgress = "progress"
	EventDone     = "done"
	EventFailed   = "failed"
	EventBlocked  = "blocked"
)

type Event struct {
	Type  string
	Step  int
	Total int
}

// Broker is in-process pub/sub for job progress (LISTEN/NOTIFY later).
type Broker struct {
	mu   sync.Mutex
	subs map[string]map[chan Event]struct{}
}

func NewBroker() *Broker {
	return &Broker{subs: make(map[string]map[chan Event]struct{})}
}

func (b *Broker) Subscribe(jobID string) (<-chan Event, func()) {
	ch := make(chan Event, 16)
	b.mu.Lock()
	if b.subs[jobID] == nil {
		b.subs[jobID] = make(map[chan Event]struct{})
	}
	b.subs[jobID][ch] = struct{}{}
	b.mu.Unlock()

	cancel := func() {
		b.mu.Lock()
		if set, ok := b.subs[jobID]; ok {
			delete(set, ch)
			if len(set) == 0 {
				delete(b.subs, jobID)
			}
		}
		b.mu.Unlock()
	}
	return ch, cancel
}

// Publish never blocks; a slow subscriber drops events. Subscribers recover
// via their periodic DB re-read, so dropped events cost latency, not truth.
func (b *Broker) Publish(jobID string, ev Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs[jobID] {
		select {
		case ch <- ev:
		default:
		}
	}
}
