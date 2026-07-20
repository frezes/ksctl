package main

import (
	"fmt"
	"os"

	kscmd "github.com/kubesphere/ksctl/pkg/cmd"
)

func main() {
	cmd := kscmd.NewKubectlPluginCommand(kscmd.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr}, kscmd.DefaultVersionInfo())
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
