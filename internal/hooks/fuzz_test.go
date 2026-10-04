package hooks

import (
	"strings"
	"testing"
)

func FuzzHookInput(f *testing.F) {
	oid := strings.Repeat("a", 40)
	f.Add("post-rewrite", "rebase", oid+" "+oid+"\n")
	f.Add("reference-transaction", "committed", oid+" "+oid+" refs/heads/main\n")
	f.Add("reference-transaction", "prepared", "broken\n")
	f.Fuzz(func(t *testing.T, event, phase, input string) {
		err := Validate(event, []string{phase}, strings.NewReader(input))
		again := Validate(event, []string{phase}, strings.NewReader(input))
		if (err == nil) != (again == nil) {
			t.Fatal("hook validation is not deterministic")
		}
		if err == nil && event != "post-merge" && event != "post-rewrite" && event != "reference-transaction" {
			t.Fatalf("accepted unknown event or wrong argument count: %q", event)
		}
	})
}
