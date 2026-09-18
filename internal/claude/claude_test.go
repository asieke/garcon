package claude

import "testing"

func TestAccountFlagIsRejected(t *testing.T) {
	for _, args := range [][]string{{"--account", "old@example.com"}, {"--account=old@example.com"}} {
		if code, _ := run(args); code != 2 {
			t.Errorf("run(%v) returned %d, want usage error", args, code)
		}
	}
}
