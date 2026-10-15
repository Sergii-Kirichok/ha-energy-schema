package config

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// topKeys returns the 2-space-indented keys of the YAML block that starts at
// the line `section:` (up to the next top-level line).
func topKeys(t *testing.T, path, section string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`^  ([a-z0-9_]+):`)
	var keys []string
	in := false
	for _, ln := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		if ln != "" && ln[0] != ' ' {
			in = ln == section+":"
			continue
		}
		if m := re.FindStringSubmatch(ln); in && m != nil {
			keys = append(keys, m[1])
		}
	}
	return keys
}

// TestTranslationsCoverSchema: every option in config.yaml schema has a name
// and description in ru.yaml and en.yaml, in the same order, and the files
// use only the simple line forms below (so they stay valid YAML).
func TestTranslationsCoverSchema(t *testing.T) {
	schema := strings.Join(topKeys(t, "../../config.yaml", "schema"), " ")
	line := regexp.MustCompile(`^(configuration:|  [a-z0-9_]+:|    (name|description): "[^"\\]*")$`)
	for _, f := range []string{"../../translations/ru.yaml", "../../translations/en.yaml"} {
		if got := strings.Join(topKeys(t, f, "configuration"), " "); got != schema {
			t.Errorf("%s keys:\n%s\nwant (schema order):\n%s", f, got, schema)
		}
		b, _ := os.ReadFile(f)
		for i, ln := range strings.Split(strings.TrimRight(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n"), "\n") {
			if !line.MatchString(ln) {
				t.Errorf("%s:%d: unexpected line %q", f, i+1, ln)
			}
		}
		if n := strings.Count(string(b), "    name: "); n != strings.Count(string(b), "    description: ") || n != len(strings.Fields(schema)) {
			t.Errorf("%s: %d names for %d schema keys (or descriptions differ)", f, n, len(strings.Fields(schema)))
		}
	}
}
