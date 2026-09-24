package auth

import (
	"testing"
	"time"
)

func TestThrottleBlocksAfterMaxFailures(t *testing.T) {
	th := NewThrottle(3, time.Minute)

	for i := range 3 {
		if ok, _ := th.Allowed("kwa"); !ok {
			t.Fatalf("blocked too early, on attempt %d", i+1)
		}
		th.Fail("kwa")
	}

	ok, retryIn := th.Allowed("kwa")
	if ok {
		t.Fatal("want block after 3 failures, got allowed")
	}
	if retryIn <= 0 {
		t.Fatalf("want a positive retry delay, got %s", retryIn)
	}
}

func TestThrottleIsPerKey(t *testing.T) {
	th := NewThrottle(1, time.Minute)
	th.Fail("kwa")

	if ok, _ := th.Allowed("kwa"); ok {
		t.Fatal("failed key should be blocked")
	}
	if ok, _ := th.Allowed("someone-else"); !ok {
		t.Fatal("a different key must not be affected")
	}
}

func TestThrottleResetClearsFailures(t *testing.T) {
	th := NewThrottle(1, time.Minute)
	th.Fail("kwa")
	th.Reset("kwa")

	if ok, _ := th.Allowed("kwa"); !ok {
		t.Fatal("Reset must clear the failure count")
	}
}

func TestThrottleForgetsAfterWindow(t *testing.T) {
	th := NewThrottle(1, 20*time.Millisecond)
	th.Fail("kwa")

	if ok, _ := th.Allowed("kwa"); ok {
		t.Fatal("want block inside the window")
	}
	time.Sleep(30 * time.Millisecond)
	if ok, _ := th.Allowed("kwa"); !ok {
		t.Fatal("want the block to lapse after the window")
	}
}
