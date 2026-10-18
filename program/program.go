package program

import (
	"path/filepath"

	"fmt"
	"github.com/deweysasser/obsidian-publish-website/internal/build"
	"github.com/deweysasser/obsidian-publish-website/internal/config"
	"io"
	"os"
	"runtime"

	"github.com/alecthomas/kong"
	"github.com/mattn/go-colorable"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var exitFunc = os.Exit

// Options is the structure of program options
type Options struct {
	Version bool `help:"Show program version"`
	// VersionCmd VersionCmd `name:"version" cmd:"" help:"show program version"`

	Debug        bool   `group:"Info" help:"Show debugging information"`
	OutputFormat string `group:"Info" enum:"auto,jsonl,terminal" default:"auto" help:"How to show program output (auto|terminal|jsonl)"`
	Quiet        bool   `group:"Info" help:"Be less verbose than usual"`

	Build BuildCmd `cmd:"" default:"withargs" help:"Build the site from a publish.yaml config"`
}

// BuildCmd builds a site from a configuration file.
type BuildCmd struct {
	Config string `short:"c" default:"publish.yaml" help:"Path to the publish config file"`
	Target string `enum:",html,hugo,jekyll" default:"" help:"Override the config's target (html|hugo|jekyll)"`
	Output string `short:"o" help:"Override the config's output directory"`
	Strict bool   `help:"Treat warnings (query errors, broken links, unsupported dataviewjs) as errors"`
}

// Parse calls the CLI parsing routines
func (program *Options) Parse(args []string) (*kong.Context, error) {
	parser, err := kong.New(program,
		kong.ShortUsageOnError(),
		kong.Description("Publish a subtree of an Obsidian vault as a website"),
	)

	if err != nil {
		fmt.Println(err)
		return nil, err
	}

	return parser.Parse(args)
}

// Run executes the build command.
func (cmd *BuildCmd) Run(logger zerolog.Logger) error {
	cfg, err := config.Load(cmd.Config)
	if err != nil {
		return err
	}
	if cmd.Target != "" {
		cfg.Target = cmd.Target
	}
	if cmd.Output != "" {
		if cfg.Output, err = filepath.Abs(cmd.Output); err != nil {
			return err
		}
	}
	cfg.Strict = cmd.Strict
	_, err = build.Run(cfg, logger)
	return err
}

// AfterApply runs after the options are parsed but before anything runs
func (program *Options) AfterApply() error {
	program.initLogging()
	return nil
}

func (program *Options) initLogging() {
	if program.Version {
		fmt.Println(Version)
		exitFunc(0)
	}

	switch {
	case program.Debug:
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case program.Quiet:
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	default:
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	var out io.Writer = os.Stdout

	if os.Getenv("TERM") == "" && runtime.GOOS == "windows" {
		out = colorable.NewColorableStdout()
	}

	if program.OutputFormat == "terminal" ||
		(program.OutputFormat == "auto" && isTerminal(os.Stdout)) {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: out})
	} else {
		log.Logger = log.Output(out)
	}

	log.Logger.Debug().
		Str("version", Version).
		Str("program", os.Args[0]).
		Msg("Starting")
}

// isTerminal returns true if the file given points to a character device (i.e. a terminal)
func isTerminal(file *os.File) bool {
	if fileInfo, err := file.Stat(); err != nil {
		log.Err(err).Msg("Error running stat")
		return false
	} else {
		return (fileInfo.Mode() & os.ModeCharDevice) != 0
	}
}
