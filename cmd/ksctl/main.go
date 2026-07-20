package main

import (
	"fmt"
	"os"

	kscmd "github.com/kubesphere/ksctl/pkg/cmd"
)

func main() {
	cmd, err := kscmd.NewRootCommandWithArgs(
		kscmd.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr},
		kscmd.DefaultVersionInfo(),
		os.Args,
	)
	if err == nil {
		err = cmd.Execute()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
