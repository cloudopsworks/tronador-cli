package replacement

import (
	"errors"
	"testing"
)

func TestReplaceDelegatesSuccessAndFailure(t *testing.T) {
	original := replace
	t.Cleanup(func() { replace = original })
	var source, destination string
	replace = func(gotSource, gotDestination string) error {
		source, destination = gotSource, gotDestination
		return nil
	}
	if err := Replace("temporary", "destination"); err != nil {
		t.Fatal(err)
	}
	if source != "temporary" || destination != "destination" {
		t.Fatalf("replace called with %q, %q", source, destination)
	}
	want := errors.New("replacement failed")
	replace = func(string, string) error { return want }
	if err := Replace("temporary", "destination"); !errors.Is(err, want) {
		t.Fatalf("Replace error = %v, want %v", err, want)
	}
}
