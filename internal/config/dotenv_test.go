package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".env")
	content := "# comment\n\nHEREZI_T_A=1\nexport HEREZI_T_B=\"two words\"\nHEREZI_T_C='x=y'\nHEREZI_T_KEEP=file\nbroken line\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HEREZI_T_KEEP", "env")
	for _, k := range []string{"HEREZI_T_A", "HEREZI_T_B", "HEREZI_T_C"} {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}
	n, err := LoadDotEnv(p)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("loaded = %d, want 3", n)
	}
	for k, want := range map[string]string{"HEREZI_T_A": "1", "HEREZI_T_B": "two words", "HEREZI_T_C": "x=y", "HEREZI_T_KEEP": "env"} {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if n, err := LoadDotEnv(filepath.Join(t.TempDir(), "missing")); n != 0 || err != nil {
		t.Errorf("missing file: n=%d err=%v", n, err)
	}
}
