// Package dotenv: minimal .env loader for native runs (KEY=VALUE, no
// expansion).
package dotenv

import (
	"os"
	"strings"
)

// Load reads the first .env found (cwd, then repo root); existing env
// always wins, matching compose precedence.
func Load() {
	for _, p := range []string{".env", "../.env"} {
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for line := range strings.SplitSeq(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			k, v = strings.TrimSpace(k), strings.TrimSpace(v)
			if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
				v = v[1 : len(v)-1]
			}
			if _, exists := os.LookupEnv(k); k != "" && !exists {
				os.Setenv(k, v)
			}
		}
		return
	}
}
