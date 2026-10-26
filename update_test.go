package main

import "testing"

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v1.0.3", "v1.0.2", true},
		{"v1.0.2", "v1.0.3", false},
		{"v1.0.3", "v1.0.3", false},
		{"v1.0.10", "v1.0.9", true},
		{"v1.1", "v1.0.9", true},
		{"v1.0", "v1.0.0", false},
		{"v1.0.3", "v1.0.3-2-gabc123", false}, // build intermedia dopo v1.0.3
		{"v1.0.4", "v1.0.3-2-gabc123", true},
		{"pippo", "v1.0", false},
	}
	for _, c := range cases {
		if got := newerVersion(c.a, c.b); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v", c.a, c.b, got)
		}
	}
	if parseVersion("dev") != nil {
		t.Error("dev non è una versione")
	}
}
