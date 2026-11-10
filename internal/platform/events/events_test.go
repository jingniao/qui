package events

import (
	"testing"
	"time"
)

func TestBusSubscribeReceivesAndUnsubscribes(t *testing.T) {
	bus := &Bus{}
	ch, unsubscribe := bus.Subscribe(4)

	bus.Publish("test_event", "hello")

	select {
	case ev := <-ch:
		if ev.Type != "test_event" || ev.Data != "hello" {
			t.Fatalf("unexpected event: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}

	unsubscribe()

	bus.Publish("test_event_2", "world")

	select {
	case ev := <-ch:
		t.Fatalf("received event after unsubscribe: %+v", ev)
	case <-time.After(50 * time.Millisecond):
		// OK
	}
}
