package ingest

import "testing"

func TestNewDriver(t *testing.T) {
	if _, err := New("chatdownloader"); err != nil {
		t.Fatalf("expected chatdownloader to be supported: %v", err)
	}
	if _, err := New("gnasty"); err != nil {
		t.Fatalf("expected gnasty to be supported (stub): %v", err)
	}
}
