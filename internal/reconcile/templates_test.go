package reconcile

import "testing"

func TestImageTag(t *testing.T) {
	cases := []struct {
		name     string
		strategy string
		pr       int
		sha      string
		want     string
		wantErr  bool
	}{
		{"sha", "sha", 1, "abcd1234", "abcd1234", false},
		{"pr", "pr", 12, "abcd1234", "pr-12", false},
		{"pr-sha", "pr-sha", 12, "abcd1234", "pr-12-abcd123", false},
		{"bad", "nope", 1, "abcd1234", "", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ImageTag(c.strategy, c.pr, c.sha)
			if c.wantErr && err == nil {
				t.Fatalf("expected error")
			}
			if !c.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestRenderTemplate(t *testing.T) {
	got, err := RenderTemplate("registry.local/{app}:{tag}", "myapp", "pr-1-abc", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "registry.local/myapp:pr-1-abc" {
		t.Fatalf("unexpected template output: %s", got)
	}
}
