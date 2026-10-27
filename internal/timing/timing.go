// Package timing adds elapsed-time fields to log events.
package timing

import (
	"time"

	"github.com/rs/zerolog"
)

// Elapsed returns a zerolog Event.Func that logs the time since start as
// "elapsed" (formatted, rounded to the millisecond) and "elapsed_ms"
// (integer milliseconds), both from the same measurement.
func Elapsed(start time.Time) func(*zerolog.Event) {
	return func(e *zerolog.Event) {
		d := time.Since(start).Round(time.Millisecond)
		e.Stringer("elapsed", d).Int64("elapsed_ms", d.Milliseconds())
	}
}
