// Package search builds the site search index with Pagefind.
package search

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/rs/zerolog"
)

// FindPagefind locates the pagefind binary on PATH.
func FindPagefind() (string, error) {
	for _, name := range []string{"pagefind", "pagefind_extended"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", errors.New(`pagefind not found on PATH; install it with "npm i -g pagefind" or from https://pagefind.app, or set "search: none"`)
}

// RunPagefind indexes siteDir in place (writes siteDir/pagefind/).
func RunPagefind(bin, siteDir string, log zerolog.Logger) error {
	cmd := exec.Command(bin, "--site", siteDir)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pagefind failed: %v\n%s", err, strings.TrimSpace(out.String()))
	}
	log.Debug().Str("output", out.String()).Msg("pagefind")
	return nil
}
