package vault

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

func birthTime(p string, st os.FileInfo) time.Time {
	var sx unix.Statx_t
	if err := unix.Statx(unix.AT_FDCWD, p, 0, unix.STATX_BTIME, &sx); err == nil && sx.Mask&unix.STATX_BTIME != 0 {
		return time.Unix(sx.Btime.Sec, int64(sx.Btime.Nsec))
	}
	return st.ModTime()
}
