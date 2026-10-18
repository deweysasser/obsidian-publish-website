//go:build !linux

package vault

import (
	"os"
	"time"
)

func birthTime(_ string, st os.FileInfo) time.Time { return st.ModTime() }
