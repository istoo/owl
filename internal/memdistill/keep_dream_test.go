package memdistill

import (
	"context"
	"testing"
	"time"
)

// The nightly tidy is a switch of its own: switched on while distilling is
// off, Keep tidies memory and reads no conversation (#42).
func TestKeepDreamsWhenDistillingOff(t *testing.T) {
	sb := newSandbox(t)
	if _, err := change(func(s *State) {
		s.On, s.Dream = false, true
		s.DreamSince = sb.at.Add(-48 * time.Hour)
		s.Dreamt = time.Time{}
	}); err != nil {
		t.Fatal(err)
	}
	// a readable session: Run would read it if it ran
	sb.session("s1", time.Hour, "hello")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		Keep(ctx, time.Millisecond, time.Millisecond, func() string { return "a/m" }, sb.complete)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if !st.Dreamt.IsZero() {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			<-done
			t.Fatal("the tidy never ran")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	st, _ := Load()
	if st.On {
		t.Fatal("distilling was switched on")
	}
	if !st.Dreamt.Equal(sb.at.UTC()) {
		t.Fatalf("tidy at %v, want %v", st.Dreamt, sb.at.UTC())
	}
	if len(st.Done) != 0 {
		t.Fatalf("distilling read sessions while off: %+v", st.Done)
	}
	if len(sb.asked) != 0 {
		t.Fatalf("a conversation was read while off: %v", sb.asked)
	}
}

// With both switched off, Keep still asks for no model: choosing one loads
// the whole catalog (3.6 MB).
func TestKeepOffAndNoDreamAsksNoModel(t *testing.T) {
	newSandbox(t)
	ctx, cancel := context.WithCancel(context.Background())
	asked := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		Keep(ctx, time.Millisecond, time.Millisecond, func() string {
			select {
			case asked <- struct{}{}:
			default:
			}
			return "m/x"
		}, nil)
	}()
	defer func() {
		cancel()
		<-done
	}()
	select {
	case <-asked:
		t.Fatal("both off, it asked for a model")
	case <-time.After(100 * time.Millisecond):
	}
}
