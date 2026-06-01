package cursor

import "testing"

func TestIsAutoModel(t *testing.T) {
	auto := []string{"", "default", "Default", "auto", "AUTO", "automatic", "  default  "}
	for _, m := range auto {
		if !IsAutoModel(m) {
			t.Fatalf("%q should be auto", m)
		}
	}
	if IsAutoModel("claude-opus-4-7") {
		t.Fatal("expected manual model")
	}
}

func TestNormalizeModel(t *testing.T) {
	if NormalizeModel("default") != "Auto" {
		t.Fatalf("got %q", NormalizeModel("default"))
	}
	if NormalizeModel("gpt-5.5") != "gpt-5.5" {
		t.Fatalf("got %q", NormalizeModel("gpt-5.5"))
	}
}
