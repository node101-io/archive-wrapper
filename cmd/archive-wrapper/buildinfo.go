package main

import (
	"fmt"
	"io"
)

var (
	version   = "dev"
	commitSHA = "unknown"
	buildDate = "unknown"
)

func writeVersion(output io.Writer) error {
	_, err := fmt.Fprintf(
		output,
		"archive-wrapper version=%s commit=%s build_date=%s\n",
		version,
		commitSHA,
		buildDate,
	)
	return err
}
