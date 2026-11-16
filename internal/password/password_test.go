package password

import (
	"testing"
	"time"
)

func TestHashesRunFourAtATime(t *testing.T) {
	h := Hash("correct horse battery")
	if !Check(h, "correct horse battery") || Check(h, "wrong password") {
		t.Fatal("hash does not check")
	}
	// Fill every slot: the next hash must wait.
	for i := 0; i < cap(slots); i++ {
		slots <- struct{}{}
	}
	done := make(chan bool)
	go func() { done <- Check(h, "correct horse battery") }()
	select {
	case <-done:
		t.Fatal("a hash ran while all slots were taken")
	case <-time.After(200 * time.Millisecond):
	}
	<-slots
	if !<-done {
		t.Fatal("check failed after a slot was freed")
	}
	for i := 1; i < cap(slots); i++ {
		<-slots
	}
}

func TestCheckRefusesForeignParameters(t *testing.T) {
	// A stored hash asking for 4 GiB of memory must not be computed.
	if Check("argon2id$1$4194304$1$c2FsdHNhbHRzYWx0c2FsdA$a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2U", "x") {
		t.Fatal("accepted")
	}
}
