package proxy

import (
	"log"
	"sync"
	"sync/atomic"

	"github.com/SpielerNogard/claude-router/internal/store"
)

type inserter interface{ Insert(store.Record) error }

type AsyncRecorder struct {
	ch     chan store.Record
	wg     sync.WaitGroup
	once   sync.Once
	mu     sync.RWMutex
	closed bool
	drops  atomic.Int64
}

func NewAsyncRecorder(s *store.Store, buffer int) *AsyncRecorder {
	return NewAsyncRecorderWith(s, buffer)
}

func NewAsyncRecorderWith(s inserter, buffer int) *AsyncRecorder {
	a := &AsyncRecorder{ch: make(chan store.Record, buffer)}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		for r := range a.ch {
			if err := s.Insert(r); err != nil {
				log.Printf("cost log: %v", err)
			}
		}
	}()
	return a
}

// Drops when the buffer is full instead of slowing down the request.
func (a *AsyncRecorder) Record(r store.Record) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.closed {
		return
	}
	select {
	case a.ch <- r:
	default:
		// Throttled logging: under sustained load every dropped row would flood the log.
		if n := a.drops.Add(1); n == 1 || n%100 == 0 {
			log.Printf("cost log: buffer full, dropped %d rows (last %s)", n, r.Alias)
		}
	}
}

func (a *AsyncRecorder) Close() {
	a.once.Do(func() {
		a.mu.Lock()
		a.closed = true
		a.mu.Unlock()
		close(a.ch)
		a.wg.Wait()
	})
}
