package roles

import (
	"slices"
	"testing"
)

func TestPick(t *testing.T) {
	cases := []struct {
		args []string
		name string
		rest []string
		ok   bool
	}{
		{[]string{"/usr/local/bin/openneko", "start"}, "", nil, false},
		{[]string{"openneko"}, "", nil, false},
		{[]string{"openneko", "graphjin", "import", "x.yml"}, "", nil, false},
		{[]string{"openneko", "graphjin", "serve"}, GraphJin, []string{"serve"}, true},
		{[]string{"openneko", "graphjin"}, GraphJin, []string{}, true},
		{[]string{"/usr/local/bin/graphjin", "serve", "--path", "/config"}, GraphJin, []string{"serve", "--path", "/config"}, true},
		{[]string{"/app/ax-harness"}, AxHarness, []string{}, true},
		{[]string{"openneko", "ax-harness", "catalog"}, AxHarness, []string{"catalog"}, true},
		{[]string{"lazy-service", "-listen", ":5001", "node"}, LazyService, []string{"-listen", ":5001", "node"}, true},
		{[]string{"openneko", "lazy-service", "node"}, LazyService, []string{"node"}, true},
		{[]string{"openneko", "--instance", "a", "graphjin", "serve"}, "", nil, false},
		{[]string{"openneko", "graphjin-config", "a", "b", "c", "d"}, GraphJinConfig, []string{"a", "b", "c", "d"}, true},
		{[]string{"openneko", "serve", "--socket", "/tmp/s.sock"}, Serve, []string{"--socket", "/tmp/s.sock"}, true},
		{[]string{"/usr/bin/serve"}, Serve, []string{}, true},
	}
	for _, c := range cases {
		name, rest, ok := Pick(c.args)
		if name != c.name || ok != c.ok || (ok && !slices.Equal(rest, c.rest)) {
			t.Errorf("Pick(%q) = %q, %q, %v; want %q, %q, %v", c.args, name, rest, ok, c.name, c.rest, c.ok)
		}
	}
}
