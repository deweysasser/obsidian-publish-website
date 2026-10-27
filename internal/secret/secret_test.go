package secret

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake op is a shell script")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "op"), []byte("#!/bin/sh\n[ \"$1\" = read ] && for a; do last=$a; done; [ \"$last\" = op://V/I/f ] && printf 'tok\\n'\n"), 0o755)
	t.Setenv("PATH", dir)
	t.Setenv("MY_TOKEN", "envtok")
	ctx := context.Background()
	for in, want := range map[string]string{"op://V/I/f": "tok", "env:MY_TOKEN": "envtok", "abc123": "abc123"} {
		if got, err := Resolve(ctx, in); err != nil || got != want {
			t.Errorf("Resolve(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := Resolve(ctx, "env:UNSET_VAR_XYZ"); err == nil || !strings.Contains(err.Error(), "UNSET_VAR_XYZ") {
		t.Errorf("unset env: %v", err)
	}
	if _, err := Resolve(ctx, "op://V/I/missing"); err == nil {
		t.Error("op failure should be an error")
	}
}

func TestIsReference(t *testing.T) {
	if !IsReference("op://a/b/c") || !IsReference("env:X") || IsReference("literal") {
		t.Error("IsReference")
	}
}
