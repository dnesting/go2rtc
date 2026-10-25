package streams

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AlexxIT/go2rtc/pkg/core"
)

type fakeProducer struct{ stopped atomic.Int64 }

func (f *fakeProducer) GetMedias() []*core.Media { return nil }
func (f *fakeProducer) GetTrack(*core.Media, *core.Codec) (*core.Receiver, error) {
	return nil, nil
}
func (f *fakeProducer) Start() error { return nil }
func (f *fakeProducer) Stop() error  { f.stopped.Add(1); return nil }

// A failing source must be dialed once, not once per waiting caller.
func TestProducerDialSingleFlightOnFailure(t *testing.T) {
	var dials atomic.Int64
	HandleFunc("dialfail", func(string) (core.Producer, error) {
		dials.Add(1)
		time.Sleep(150 * time.Millisecond)
		return nil, errors.New("unreachable")
	})

	p := NewProducer("dialfail://x")

	const n = 50
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = p.Dial()
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err == nil {
			t.Fatalf("caller %d got nil, want the shared dial error", i)
		}
	}
	if got := dials.Load(); got != 1 {
		t.Fatalf("dials = %d, want 1 (callers should share one in-flight dial)", got)
	}
}

// Concurrent callers on a working source share one dial and all succeed.
func TestProducerDialSingleFlightOnSuccess(t *testing.T) {
	var dials atomic.Int64
	HandleFunc("dialok", func(string) (core.Producer, error) {
		dials.Add(1)
		time.Sleep(100 * time.Millisecond)
		return &fakeProducer{}, nil
	})

	p := NewProducer("dialok://x")

	const n = 50
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = p.Dial()
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("caller %d: %v", i, err)
		}
	}
	if got := dials.Load(); got != 1 {
		t.Fatalf("dials = %d, want 1", got)
	}
	if p.state != stateMedias {
		t.Fatalf("state = %v, want stateMedias", p.state)
	}
}

// Dial must not wedge or install a stale conn when the producer is stopped
// underneath it. Run with -race.
func TestProducerDialRacesWithStop(t *testing.T) {
	HandleFunc("dialrace", func(string) (core.Producer, error) {
		time.Sleep(time.Millisecond)
		return &fakeProducer{}, nil
	})

	p := NewProducer("dialrace://x")

	var wg sync.WaitGroup
	done := make(chan struct{})

	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					_ = p.Dial()
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				p.mu.Lock()
				if p.state == stateMedias {
					p.state = stateTracks // let stop() past its stateNone guard
				}
				p.mu.Unlock()
				p.stop()
			}
		}
	}()

	time.Sleep(500 * time.Millisecond)
	close(done)
	wg.Wait()

	// If a dial were never released, callers above would still be blocked.
}
