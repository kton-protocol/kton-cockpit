package config

import "testing"

// index.memory is read in the engine's notation, and one limit is compared with another so a
// configuration that asks for more restarts the daemon and one that asks for less does not.
func TestConfig_IndexMemoryIsALimitThatCanBeCompared(t *testing.T) {
	for s, want := range map[string]int64{"2g": 2 << 30, "8G": 8 << 30, "512m": 512 << 20, "100k": 100 << 10, "4096b": 4096, "4096": 4096} {
		if got, ok := MemoryBytes(s); !ok || got != want {
			t.Errorf("%q: got %d %v, want %d", s, got, ok, want)
		}
	}
	for _, s := range []string{"", "lots", "2x", "-1g", "0", "g"} {
		if _, ok := MemoryBytes(s); ok {
			t.Errorf("%q: read as a limit", s)
		}
	}
	if !MemoryAbove("8g", "2g") || MemoryAbove("2g", "2048m") || MemoryAbove("1g", "2g") || MemoryAbove("lots", "2g") {
		t.Error("MemoryAbove compares the limits, and an unreadable one is never above")
	}
}
