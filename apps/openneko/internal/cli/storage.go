package cli

import (
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"

	"github.com/open-neko/neko/apps/openneko/internal/db"
)

func newStorageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "storage",
		Short: "Inspect or reconcile the enforced database storage contract",
	}
	cmd.AddCommand(newStorageContractCmd(), newStorageReconcileCmd())
	return cmd
}

func newStorageContractCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "contract",
		Short: "Print the storage contract compiled into this OpenNeko release",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), db.StorageContractVersion)
		},
	}
}

func newStorageReconcileCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reconcile",
		Short: "Reconcile the managed database before application startup",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			out := cmd.OutOrStdout()
			target, err := defaultConn()
			if err != nil {
				return err
			}
			conn, err := pgx.Connect(ctx, target.DSN())
			if err != nil {
				return fmt.Errorf("connect to database: %w", err)
			}
			repaired, reconcileErr := db.ReconcileStorage(ctx, conn, func(format string, args ...any) {
				fmt.Fprintf(out, format+"\n", args...)
			})
			closeErr := conn.Close(ctx)
			if reconcileErr != nil {
				return fmt.Errorf("reconcile database: %w", reconcileErr)
			}
			if closeErr != nil {
				return fmt.Errorf("close database connection: %w", closeErr)
			}
			if repaired {
				fmt.Fprintln(out, "database storage repaired")
			}
			fmt.Fprintf(out, "database satisfies storage contract %d\n", db.StorageContractVersion)
			return nil
		},
	}
}
