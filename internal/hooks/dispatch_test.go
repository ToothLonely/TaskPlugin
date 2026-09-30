package hooks

import (
	"strings"
	"testing"
)

func TestEventContracts(t *testing.T) {
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	for _, tc := range []struct {
		event string
		args  []string
		input string
		valid bool
	}{
		{"post-commit", nil, "unread", true},
		{"post-commit", []string{"extra"}, "", false},
		{"post-merge", []string{"0"}, "", true},
		{"post-merge", []string{"1"}, "", true},
		{"post-merge", []string{"2"}, "", false},
		{"post-checkout", []string{strings.Repeat("0", 40), b, "1"}, "", true},
		{"post-checkout", []string{a, b, "0"}, "", true},
		{"post-checkout", []string{a, b, "2"}, "", false},
		{"post-checkout", []string{"--evil", b, "1"}, "", false},
		{"post-rewrite", []string{"amend"}, a + " " + b + "\n", true},
		{"post-rewrite", []string{"rebase", "future"}, a + " " + b + " extra info\n" + b + " " + a + "\n", true},
		{"post-rewrite", []string{"rebase"}, "", true},
		{"post-rewrite", []string{"rebase"}, "broken\n", false},
		{"post-rewrite", []string{"unknown"}, "", false},
		{"post-rewrite", []string{"amend"}, a + " " + strings.Repeat("a", 64) + "\n", false},
		{"unknown", nil, "", false},
	} {
		err := Validate(tc.event, tc.args, strings.NewReader(tc.input))
		if (err == nil) != tc.valid {
			t.Fatalf("%s %q input=%q: %v", tc.event, tc.args, tc.input, err)
		}
	}
	if Validate("post-rewrite", []string{"amend"}, nil) == nil {
		t.Fatal("nil rewrite input accepted")
	}
}
