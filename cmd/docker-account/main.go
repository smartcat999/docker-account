package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/smartcat999/docker-account/internal/cli"
)

var version = "dev"

func main() {
	if strings.HasPrefix(filepath.Base(os.Args[0]), "docker-credential-docker-account") {
		os.Exit(cli.RunCredentialHelper(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
	}
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, version))
}
