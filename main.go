package main

import (
	"fmt"
	"os"

	"github.com/01builders/da-monitor/cmd"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "da-monitor",
		Short: "DA monitoring tool for ev-node data availability",
	}

	rootCmd.AddCommand(cmd.NewMonitorCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
