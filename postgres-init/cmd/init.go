package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/mediamoth/mediamoth-images/postgres-init/internal/config"
	"github.com/mediamoth/mediamoth-images/postgres-init/internal/pgx5"
	"github.com/spf13/cobra"
)

func newInitCommand() *cobra.Command {
	command := &cobra.Command{
		Use: "init", Short: "Ensure configured PostgreSQL resources exist", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			path, err := command.Flags().GetString("config")
			if err != nil {
				return err
			}
			cfg, err := config.Load(config.ConfigPath(path))
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(command.Context(), time.Minute)
			defer cancel()
			if err := pgx5.Init(ctx, cfg); err != nil {
				return err
			}
			_, err = fmt.Fprintln(command.OutOrStdout(), "PostgreSQL resources initialized.")
			return err
		},
	}
	command.Flags().String("config", string(config.DefaultPath), "YAML configuration file")
	return command
}
