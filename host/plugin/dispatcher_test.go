package plugin

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	sdk "github.com/sbgayhub/golem/sdk/plugin"
)

type eventCallRecorder struct {
	mu    sync.Mutex
	names []string
}

func (r *eventCallRecorder) add(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.names = append(r.names, name)
}

func (r *eventCallRecorder) values() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	values := make([]string, len(r.names))
	copy(values, r.names)
	return values
}

type recordingEventPlugin struct {
	name       string
	recorder   *eventCallRecorder
	handled    bool
	err        error
	panicValue any
	block      <-chan struct{}
	started    chan<- struct{}
	finished   chan<- struct{}
}

func (p *recordingEventPlugin) GetSubscriptions() []string {
	return []string{"message::"}
}

func (p *recordingEventPlugin) OnEvent(*sdk.Event) (bool, error) {
	p.recorder.add(p.name)
	if p.started != nil {
		close(p.started)
	}
	if p.finished != nil {
		defer close(p.finished)
	}
	if p.block != nil {
		<-p.block
	}
	if p.panicValue != nil {
		panic(p.panicValue)
	}
	return p.handled, p.err
}

func TestDispatchEventStopsAfterHandledPluginWithNextFalse(t *testing.T) {
	recorder := &eventCallRecorder{}
	plugins := []*wrapper{
		eventWrapper("first", true, &recordingEventPlugin{name: "first", recorder: recorder, handled: true}),
		eventWrapper("second", false, &recordingEventPlugin{name: "second", recorder: recorder, handled: true}),
		eventWrapper("third", false, &recordingEventPlugin{name: "third", recorder: recorder, handled: true}),
	}

	dispatchEvent(testDispatchEvent(), plugins)

	assertEventCalls(t, recorder, []string{"first", "second"})
}

func TestDispatchEventContinuesAfterUnsuccessfulPlugin(t *testing.T) {
	recorder := &eventCallRecorder{}
	plugins := []*wrapper{
		eventWrapper("not-handled", false, &recordingEventPlugin{name: "not-handled", recorder: recorder}),
		eventWrapper("failed", false, &recordingEventPlugin{name: "failed", recorder: recorder, handled: true, err: errors.New("failed")}),
		eventWrapper("panicked", false, &recordingEventPlugin{name: "panicked", recorder: recorder, panicValue: "boom"}),
		eventWrapper("handled", false, &recordingEventPlugin{name: "handled", recorder: recorder, handled: true}),
		eventWrapper("skipped", false, &recordingEventPlugin{name: "skipped", recorder: recorder, handled: true}),
	}

	dispatchEvent(testDispatchEvent(), plugins)

	assertEventCalls(t, recorder, []string{"not-handled", "failed", "panicked", "handled"})
}

func TestDispatchEventContinuesAfterTimeout(t *testing.T) {
	oldTimeout := eventPluginTimeout
	eventPluginTimeout = 10 * time.Millisecond
	t.Cleanup(func() {
		eventPluginTimeout = oldTimeout
	})

	recorder := &eventCallRecorder{}
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	var releaseOnce sync.Once
	releaseSlowPlugin := func() {
		releaseOnce.Do(func() {
			close(release)
		})
	}
	t.Cleanup(releaseSlowPlugin)

	plugins := []*wrapper{
		eventWrapper("slow", false, &recordingEventPlugin{
			name:     "slow",
			recorder: recorder,
			handled:  true,
			block:    release,
			started:  started,
			finished: finished,
		}),
		eventWrapper("next", false, &recordingEventPlugin{name: "next", recorder: recorder, handled: true}),
	}

	done := make(chan struct{})
	go func() {
		dispatchEvent(testDispatchEvent(), plugins)
		close(done)
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("slow plugin was not called")
	}

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("dispatchEvent did not continue after plugin timeout")
	}

	assertEventCalls(t, recorder, []string{"slow", "next"})

	releaseSlowPlugin()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("slow plugin did not finish after release")
	}
}

func eventWrapper(name string, next bool, eventPlugin sdk.EventPlugin) *wrapper {
	return &wrapper{
		Metadata:      &sdk.Metadata{Name: name, Next: next},
		Config:        &Config{Enable: true, Mode: "blacklist"},
		subscriptions: eventPlugin.GetSubscriptions(),
		eventPlugin:   &eventPlugin,
	}
}

func testDispatchEvent() *sdk.Event {
	return &sdk.Event{Topic: "message::receive"}
}

func assertEventCalls(t *testing.T, recorder *eventCallRecorder, want []string) {
	t.Helper()

	if got := recorder.values(); !slices.Equal(got, want) {
		t.Fatalf("event calls = %v, want %v", got, want)
	}
}
