package main

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestInterruptContextCancelsOnInterrupt pins that an interrupt cancels the
// command's context rather than ending the process, so a remediation it stops
// removes its temp files on the way out.
func TestInterruptContextCancelsOnInterrupt(t *testing.T) {
	ctx, stop := interruptContext(context.Background())
	defer stop()

	self, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatalf("FindProcess: %v", err)
	}
	if err := self.Signal(os.Interrupt); err != nil {
		t.Skipf("this platform cannot send itself an interrupt: %v", err)
	}
	select {
	case <-ctx.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the interrupt did not cancel the context")
	}
}
