package timing

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestElapsedLogsFormattedAndMilliseconds(t *testing.T) {
	var buf bytes.Buffer
	log := zerolog.New(&buf)
	log.Info().Func(Elapsed(time.Now().Add(-1500 * time.Millisecond))).Msg("x")
	var got struct {
		Elapsed   string `json:"elapsed"`
		ElapsedMS *int64 `json:"elapsed_ms"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ElapsedMS == nil || *got.ElapsedMS < 1500 || *got.ElapsedMS > 2500 {
		t.Fatalf("elapsed_ms %v in %s", got.ElapsedMS, buf.String())
	}
	if d, err := time.ParseDuration(got.Elapsed); err != nil || d.Milliseconds() != *got.ElapsedMS {
		t.Errorf("elapsed %q (%v) does not match elapsed_ms %d", got.Elapsed, err, *got.ElapsedMS)
	}
}
