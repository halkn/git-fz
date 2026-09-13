package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"github.com/halkn/git-fz/internal/command"
	"github.com/halkn/git-fz/internal/fzf"
	"github.com/halkn/git-fz/internal/git"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "git-fz: cannot determine executable path: %v\n", err)
		os.Exit(1)
	}

	runner := command.NewRunner(git.New(""), fzf.New(""), executable)
	if err := runner.Run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, fzf.ErrCancelled) {
			return
		}
		fmt.Fprintf(os.Stderr, "git-fz: %v\n", err)
		os.Exit(1)
	}
}
