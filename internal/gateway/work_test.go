package gateway

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/DIYgod/pinky/internal/demo"
	"github.com/DIYgod/pinky/internal/driver"
)

// workDevice is a demo phone that hears whether an agent works on it, as
// Android's driver does for Pinky Companion.
type workDevice struct {
	*demo.Device
	mu    sync.Mutex
	calls []bool
}

func (w *workDevice) AgentWorking(on bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, on)
}

func (w *workDevice) heard() []bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.calls)
}

// workProvider opens the workDevice in place of its demo phone.
type workProvider struct {
	*demo.Provider
	dev *workDevice
}

func (p workProvider) Open(context.Context, string) (driver.Device, error) { return p.dev, nil }

func TestAgentWorking(t *testing.T) {
	w := &workDevice{Device: demo.NewDevice("pixel", "Pixel 9", driver.Android)}
	g, _ := newTestGateway(t, func(o *Options) {
		o.Providers = []driver.Provider{workProvider{demo.NewProvider(driver.Android, w.Device), w}}
	})
	waitOnline(t, g, "pixel")
	want := func(step string, calls ...bool) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for !slices.Equal(w.heard(), calls) {
			if time.Now().After(deadline) {
				t.Fatalf("%s: the driver heard %v, want %v", step, w.heard(), calls)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	want("opened with no task", false)

	s, _ := connect(t, g, "Check the weather")
	want("a task started", false, true)

	// Another call of the same task changes nothing.
	s.Connect(context.Background(), ConnectRequest{Task: "Check the weather"})
	want("the same task again", false, true)

	if err := g.TakeOver("pixel"); err != nil {
		t.Fatal(err)
	}
	want("the person took over", false, true, false)
	if err := g.HandBack("pixel", ""); err != nil {
		t.Fatal(err)
	}
	want("the person handed back", false, true, false, true)

	if res := s.Release(context.Background(), ReleaseRequest{}); res.IsError() {
		t.Fatalf("release: %+v", res)
	}
	want("the task ended", false, true, false, true, false)

	// A task that starts while the person is in control starts paused:
	// no agent works until they hand back.
	if err := g.TakeOver("pixel"); err != nil {
		t.Fatal(err)
	}
	s2 := g.OpenSession(agent)
	t.Cleanup(s2.Close)
	s2.Connect(context.Background(), ConnectRequest{Task: "Read the news"})
	time.Sleep(50 * time.Millisecond)
	want("a task under the person's control", false, true, false, true, false)
	if err := g.HandBack("pixel", ""); err != nil {
		t.Fatal(err)
	}
	want("handed back to the new task", false, true, false, true, false, true)
}
