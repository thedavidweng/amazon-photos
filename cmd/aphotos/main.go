package main

import (
	"errors"
	"os"

	"github.com/thedavidweng/amazon-photos/internal/cli"
	"github.com/thedavidweng/amazon-photos/internal/model"
)

func main() {
	if err := cli.Execute(); err != nil {
		var cmdErr *model.CommandError
		if errors.As(err, &cmdErr) {
			os.Exit(cmdErr.ExitCode())
		}
		os.Exit(1)
	}
}
