package main

import (
	"errors"
	"os"

	"github.com/thedavidweng/amazon-photos/internal/cli"
	"github.com/thedavidweng/amazon-photos/internal/model"
)

func main() {
	if err := cli.Execute(); err != nil {
		if cmdErr, ok := errors.AsType[*model.CommandError](err); ok {
			os.Exit(cmdErr.ExitCode())
		}
		os.Exit(1)
	}
}
