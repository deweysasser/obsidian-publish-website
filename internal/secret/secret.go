// Package secret resolves credential references from the config: an
// "op://vault/item/field" 1Password reference (read with the op CLI), or
// "env:NAME" for an environment variable. Anything else is a literal value.
package secret

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// IsReference reports whether v names a secret rather than containing one.
func IsReference(v string) bool {
	return strings.HasPrefix(v, "op://") || strings.HasPrefix(v, "env:")
}

// Resolve returns the value v refers to.
func Resolve(ctx context.Context, v string) (string, error) {
	switch {
	case strings.HasPrefix(v, "op://"):
		cmd := exec.CommandContext(ctx, "op", "read", "--no-newline", v)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("op read %s: %v: %s", v, err, strings.TrimSpace(stderr.String()))
		}
		return strings.TrimRight(string(out), "\r\n"), nil
	case strings.HasPrefix(v, "env:"):
		name := strings.TrimPrefix(v, "env:")
		val := os.Getenv(name)
		if val == "" {
			return "", fmt.Errorf("environment variable %s is not set", name)
		}
		return val, nil
	}
	return v, nil
}
