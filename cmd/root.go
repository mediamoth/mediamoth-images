package cmd

import (
	"context"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:               "postgres-init",
	Short:             "Create missing PostgreSQL users, databases, schemas, and schema grants",
	SilenceUsage:      true,
	SilenceErrors:     true,
	CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
}

func init() { rootCmd.AddCommand(newInitCommand()) }

func Execute(ctx context.Context) error { return rootCmd.ExecuteContext(ctx) }
