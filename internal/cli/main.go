package cli

import (
	"context"
	"io"
	"os"
)

type ExitCode int

const (
	ExitOK    ExitCode = 0
	ExitUsage ExitCode = 2
	ExitError ExitCode = 1
)

type App struct {
	Out io.Writer
	Err io.Writer
}

func Main(args []string) int {
	app := &App{
		Out: os.Stdout,
		Err: os.Stderr,
	}
	return int(app.Run(context.Background(), args))
}

func (a *App) Run(ctx context.Context, args []string) ExitCode {
	root := newRootCmd(rootOpts{
		Out: a.Out,
		Err: a.Err,
	})
	root.SetArgs(args)
	root.SetContext(ctx)

	if err := root.Execute(); err != nil {
		return ExitError
	}
	return ExitOK
}
