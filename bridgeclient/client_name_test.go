package bridge

import (
	"context"
	"testing"
)

// The command log names each command's sender from this field (spec,
// "Sender identity in the log").
func TestClientSendsItsName(t *testing.T) {
	c, frames := captureFrames(t)
	c.Name = "terraform"
	if _, err := c.Request(context.Background(), "get_status", nil); err != nil {
		t.Fatalf("request: %v", err)
	}
	if got := (<-frames)["client"]; got != "terraform" {
		t.Fatalf("frame client = %v, want terraform", got)
	}
}

func TestUnnamedClientSendsNoName(t *testing.T) {
	c, frames := captureFrames(t)
	if _, err := c.Request(context.Background(), "get_status", nil); err != nil {
		t.Fatalf("request: %v", err)
	}
	if frame := <-frames; frame["client"] != nil {
		t.Fatalf("frame carries client %v; an unnamed client must send none", frame["client"])
	}
}
