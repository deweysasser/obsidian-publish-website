package program

import (
	"os/exec"
	"path/filepath"

	"github.com/rs/zerolog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/zenizh/go-capturer"
)

func TestOptions_Run(t *testing.T) {
	var program Options

	exitValue := -1
	exitFunc = func(x int) {
		exitValue = x
	}
	defer func() { exitFunc = os.Exit }()

	out := capturer.CaptureStdout(func() {

		_, err := program.Parse([]string{"--version"})

		assert.NoError(t, err)

		// version output is done as part of parsing, so we don't need to run the program
	})

	assert.Equal(t, exitValue, 0)
	assert.Equal(t, "unknown\n", out)
}

func TestParseBuildCommand(t *testing.T) {
	var program Options
	ctx, err := program.Parse([]string{"build", "-c", "x.yaml", "--target", "hugo", "-o", "out", "--strict"})
	assert.NoError(t, err)
	assert.Equal(t, "build", ctx.Command())
	assert.Equal(t, "x.yaml", program.Build.Config)
	assert.Equal(t, "hugo", program.Build.Target)
	assert.Equal(t, "out", program.Build.Output)
	assert.True(t, program.Build.Strict)
}

func TestBuildCommandRejectsBadTarget(t *testing.T) {
	var program Options
	_, err := program.Parse([]string{"build", "--target", "pdf"})
	assert.Error(t, err)
}

func TestBuildCommandRun(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not on PATH")
	}
	dir := t.TempDir()
	vault, _ := filepath.Abs("../testdata/fixture-vault")
	cfg := filepath.Join(dir, "publish.yaml")
	os.WriteFile(cfg, []byte("vault: "+vault+"\nroot: Town\nexclude: [People/**]\nsearch: none\nsite: {title: T, base_url: https://x.io/}\n"), 0o644)
	out := filepath.Join(dir, "out")
	cmd := BuildCmd{Config: cfg, Output: out}
	assert.NoError(t, cmd.Run(zerolog.Nop()))
	_, err := os.Stat(filepath.Join(out, "issues/road-repair/index.html"))
	assert.NoError(t, err)

	broken := filepath.Join(dir, "v")
	os.MkdirAll(filepath.Join(broken, "R"), 0o755)
	os.WriteFile(filepath.Join(broken, "R", "a.md"), []byte("[[missing]]"), 0o644)
	cfg2 := filepath.Join(dir, "p2.yaml")
	os.WriteFile(cfg2, []byte("vault: "+broken+"\nroot: R\nsearch: none\n"), 0o644)
	cmd = BuildCmd{Config: cfg2, Output: filepath.Join(dir, "out2")}
	assert.NoError(t, cmd.Run(zerolog.Nop()))
	cmd = BuildCmd{Config: cfg2, Output: filepath.Join(dir, "out3"), Strict: true}
	assert.ErrorContains(t, cmd.Run(zerolog.Nop()), "R/a.md:1")
}
