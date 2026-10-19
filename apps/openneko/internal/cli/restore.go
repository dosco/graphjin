package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

func newRestoreCmd() *cobra.Command {
	var target string
	var confirmation string
	cmd := &cobra.Command{
		Use:   "restore --to <timestamp> --confirm RESTORE",
		Short: "Restore metadata, records, and config to a coordinated point in time",
		Long: `Stop every database consumer, restore the latest complete backup set at or
before the requested UTC timestamp, replay both WAL streams, and reconcile the
records schema before reopening web traffic. This overwrites the live database
and configuration volumes and requires the latest backup set to have passed a
fresh whole-deployment restore verification.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			normalized, err := normalizeRestoreTarget(target, time.Now())
			if err != nil {
				return err
			}
			if err := validateRestoreConfirmation(confirmation); err != nil {
				return err
			}
			return runRestore(cmd.Context(), cmd, normalized)
		},
	}
	cmd.Flags().StringVar(&target, "to", "", "UTC RFC3339 recovery target (required)")
	cmd.Flags().StringVar(&confirmation, "confirm", "", "type RESTORE to allow destructive overwrite")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

func validateRestoreConfirmation(raw string) error {
	if raw != "RESTORE" {
		return fmt.Errorf("restore overwrites live data and configuration; pass --confirm RESTORE exactly")
	}
	return nil
}

func normalizeRestoreTarget(raw string, now time.Time) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("--to is required")
	}
	target, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return "", fmt.Errorf("--to must be an RFC3339 timestamp with a timezone: %w", err)
	}
	target = target.UTC()
	if target.After(now.UTC()) {
		return "", fmt.Errorf("--to cannot be in the future")
	}
	return target.Format(time.RFC3339Nano), nil
}

func runRestore(ctx context.Context, cmd *cobra.Command, target string) error {
	sup, _, project, files, err := currentStackCompose()
	if err != nil {
		return err
	}
	if err := requireFreshVerifiedBackupFor(ctx, project, files, time.Now(), "restore"); err != nil {
		return err
	}

	stopServices := []string{
		"stop",
		"web",
		"worker",
		"neko-graphjin",
		"records-graphjin",
		"records-watch-graphjin",
		"neko-backup",
		"neko-db",
		"records-db",
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Stopping database consumers...")
	if code, err := sup.Run(ctx, project, files, stopServices, os.Stdout, os.Stderr); err != nil {
		return err
	} else if code != 0 {
		return WithExit(code, nil)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Restoring coordinated state to %s...\n", target)
	restoreArgs := []string{
		"--profile", "ops", "run", "--rm", "--no-deps",
		"neko-restore", "restore", "--to", target,
	}
	if code, err := sup.Run(ctx, project, files, restoreArgs, os.Stdout, os.Stderr); err != nil {
		return err
	} else if code != 0 {
		return WithExit(code, fmt.Errorf("restore failed; stack remains stopped"))
	}

	// Bringing up worker alone starts its database/migration dependencies. Its
	// health endpoint binds only after records schema recovery reconciliation,
	// so --wait establishes the cutover fence before web can serve traffic.
	fmt.Fprintln(cmd.OutOrStdout(), "Reconciling restored records state...")
	workerArgs := []string{"up", "-d", "--wait", "--wait-timeout", "180", "worker"}
	if code, err := sup.Run(ctx, project, files, workerArgs, os.Stdout, os.Stderr); err != nil {
		return err
	} else if code != 0 {
		return WithExit(code, fmt.Errorf("restore succeeded but reconciliation failed; web remains stopped"))
	}

	if code, err := sup.Run(ctx, project, files, []string{"up", "-d"}, os.Stdout, os.Stderr); err != nil {
		return err
	} else if code != 0 {
		return WithExit(code, fmt.Errorf("restore and reconciliation succeeded but stack restart failed"))
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Restore complete at %s; records reconciliation passed before traffic reopened.\n", target)
	return nil
}

func requireFreshVerifiedBackupFor(
	ctx context.Context,
	project string,
	files []string,
	now time.Time,
	operation string,
) error {
	raw, err := composeCommandOutput(ctx, project, files,
		"exec", "-T", "neko-backup", "curl", "-fsS",
		"http://127.0.0.1:9470/v1/backups/status")
	if err != nil {
		return fmt.Errorf("%s refused: cannot verify backup protection: %w", operation, err)
	}
	var status backupDoctorStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		return fmt.Errorf("%s refused: invalid backup status: %w", operation, err)
	}
	if ok, detail := backupRestoreProtection(status, now); !ok {
		return fmt.Errorf("%s refused: %s; run `openneko backup now` followed by `openneko backup verify`", operation, detail)
	}
	return nil
}
