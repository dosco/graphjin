package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/open-neko/neko/apps/openneko/internal/host"
	"github.com/open-neko/neko/apps/openneko/internal/preflight"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check whether this host can run the OpenShell sandbox runtime and report state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			h := host.Check()
			supportedTag := "supported"
			if !h.Supported {
				supportedTag = "UNSUPPORTED"
			}
			fmt.Fprintf(out, "host: %s (%s)\n", h.Triple, supportedTag)
			if !h.Supported && h.Reason != "" {
				fmt.Fprintf(out, "  reason: %s\n", h.Reason)
			}
			docker := preflight.Docker()
			fmt.Fprintf(out, "docker:   %s\n", docker.Detail)
			if docker.Level == preflight.Fail && docker.Remediation != "" {
				fmt.Fprintf(out, "  fix: %s\n", docker.Remediation)
			}

			if dup := preflight.DuplicateBinary(); dup.Level == preflight.Warn {
				fmt.Fprintf(out, "warning:  %s — %s\n", dup.Detail, dup.Remediation)
			}

			operationalFailed := false
			if docker.Level == preflight.Pass {
				failed, err := runOperationalDoctor(cmd.Context(), out)
				if err != nil {
					fmt.Fprintf(out, "operations: FAIL — %v\n", err)
					operationalFailed = true
				} else {
					operationalFailed = failed
				}
			}

			if !h.Supported || operationalFailed {
				return WithExit(1, nil)
			}
			return nil
		},
	}
}

type storageCapacity struct {
	TotalBytes  float64 `json:"totalBytes"`
	FreeBytes   float64 `json:"freeBytes"`
	UsedPercent float64 `json:"usedPercent"`
}

type storageStatus struct {
	Metadata storageCapacity `json:"metadata"`
	Records  storageCapacity `json:"records"`
	Staging  storageCapacity `json:"staging"`
}

type backupDoctorStatus struct {
	State                  string `json:"state"`
	LastBackupAt           string `json:"last_backup_at"`
	LastBackupSet          string `json:"last_backup_set"`
	LastVerificationAt     string `json:"last_verification_at"`
	LastVerificationStatus string `json:"last_verification_status"`
	Verification           struct {
		Status    string `json:"status"`
		BackupSet string `json:"backup_set"`
		Databases []struct {
			Stanza string `json:"stanza"`
		} `json:"databases"`
		ConfigSnapshot struct {
			Status string `json:"status"`
		} `json:"config_snapshot"`
	} `json:"verification"`
}

func runOperationalDoctor(ctx context.Context, out io.Writer) (bool, error) {
	_, _, project, files, err := currentStackCompose()
	if err != nil {
		return false, err
	}
	if warning := backupFailureDomainWarning(); warning != "" {
		fmt.Fprintf(out, "backup target: warning — %s\n", warning)
	}
	services, err := composePs(project, files)
	if err != nil {
		return false, err
	}
	running := map[string]bool{}
	for _, service := range services {
		running[service.Service] = service.State == "running"
	}
	if len(services) == 0 {
		fmt.Fprintln(out, "operations: stack not running (database, storage, and backup checks skipped)")
		return false, nil
	}

	failed := false
	if !running["records-db"] {
		fmt.Fprintln(out, "records:  FAIL — records-db is not running")
		failed = true
	} else if _, err := composeCommandOutput(ctx, project, files,
		"exec", "-T", "records-db", "sh", "-ec",
		`pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"`); err != nil {
		fmt.Fprintf(out, "records:  FAIL — %v\n", err)
		failed = true
	} else {
		fmt.Fprintln(out, "records:  reachable")
		maintenance, err := composeCommandOutput(ctx, project, files,
			"exec", "-T", "records-db", "sh", "-ec",
			`psql -X -Atq -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "select current_setting('autovacuum') || '|' || coalesce(round(100 * max(n_dead_tup::numeric / nullif(n_live_tup + n_dead_tup, 0)), 1), 0)::text from pg_stat_user_tables"`)
		if err != nil {
			fmt.Fprintf(out, "maintenance: warning — %v\n", err)
		} else {
			parts := strings.Split(strings.TrimSpace(string(maintenance)), "|")
			if len(parts) == 2 {
				fmt.Fprintf(out, "maintenance: autovacuum %s; worst dead-row ratio %s%%\n", parts[0], parts[1])
			}
		}
	}

	if !running["neko-backup"] {
		fmt.Fprintln(out, "storage:  FAIL — neko-backup is not running")
		failed = true
	} else {
		raw, err := composeCommandOutput(ctx, project, files,
			"exec", "-T", "neko-backup", "curl", "-fsS", "http://127.0.0.1:9470/v1/storage")
		if err != nil {
			fmt.Fprintf(out, "storage:  FAIL — %v\n", err)
			failed = true
		} else {
			var status storageStatus
			if err := json.Unmarshal(raw, &status); err != nil {
				return false, fmt.Errorf("parse storage status: %w", err)
			}
			for _, volume := range []struct {
				name     string
				capacity storageCapacity
			}{
				{"metadata", status.Metadata},
				{"records", status.Records},
				{"staging", status.Staging},
			} {
				level := storageDoctorLevel(volume.capacity)
				fmt.Fprintf(out, "storage %s: %.1f GiB free, %.1f%% used (%s)\n",
					volume.name, volume.capacity.FreeBytes/(1<<30), volume.capacity.UsedPercent, level)
				if level == "critical" || level == "hard stop" {
					failed = true
				}
			}
		}
	}

	if !running["neko-backup"] {
		fmt.Fprintln(out, "backup:   FAIL — neko-backup is not running")
		return true, nil
	}
	raw, err := composeCommandOutput(ctx, project, files,
		"exec", "-T", "neko-backup", "curl", "-fsS", "http://127.0.0.1:9470/v1/backups/status")
	if err != nil {
		fmt.Fprintf(out, "backup:   FAIL — %v\n", err)
		return true, nil
	}
	var backup backupDoctorStatus
	if err := json.Unmarshal(raw, &backup); err != nil {
		return false, fmt.Errorf("parse backup status: %w", err)
	}
	backupOK, backupDetail := backupDoctorHealth(backup, time.Now())
	if !backupOK {
		failed = true
	}
	fmt.Fprintf(out, "backup:   %s\n", backupDetail)

	walCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	for _, stanza := range []string{"openneko-metadata", "openneko-records"} {
		_, err := composeCommandOutput(walCtx, project, files,
			"exec", "-T", "neko-backup", "sh", "-ec",
			`unset PGBACKREST_REPO1_CIPHER_PASS_FILE
if command -v gosu >/dev/null 2>&1; then exec gosu postgres "$@"; fi
exec su-exec postgres "$@"`, "openneko-run-as-postgres", "pgbackrest",
			"--log-level-console=warn", "--stanza="+stanza, "check")
		if err != nil {
			fmt.Fprintf(out, "WAL %s: FAIL — %v\n", stanza, err)
			failed = true
		} else {
			fmt.Fprintf(out, "WAL %s: archived\n", stanza)
		}
	}
	return failed, nil
}

func storageDoctorLevel(capacity storageCapacity) string {
	switch {
	case capacity.UsedPercent >= 95 || capacity.FreeBytes <= 1*(1<<30):
		return "hard stop"
	case capacity.UsedPercent >= 90 || capacity.FreeBytes <= 2*(1<<30):
		return "critical"
	case capacity.UsedPercent >= 80 || capacity.FreeBytes <= 5*(1<<30):
		return "warning"
	default:
		return "ok"
	}
}

func backupDoctorHealth(status backupDoctorStatus, now time.Time) (bool, string) {
	if status.State != "ready" {
		return false, fmt.Sprintf("FAIL — backup service state is %q", status.State)
	}
	backupAt, err := time.Parse(time.RFC3339, status.LastBackupAt)
	if err != nil {
		return false, "FAIL — no completed whole-deployment backup"
	}
	backupAge := now.Sub(backupAt)
	verifiedAt, verifyErr := time.Parse(time.RFC3339, status.LastVerificationAt)
	if status.LastVerificationStatus != "succeeded" || verifyErr != nil {
		return false, fmt.Sprintf("FAIL — latest backup age %s; no successful restore verification", durationAge(backupAge))
	}
	verificationAge := now.Sub(verifiedAt)
	if backupAge > 36*time.Hour {
		return false, fmt.Sprintf("FAIL — latest backup is stale (%s old)", durationAge(backupAge))
	}
	if verificationAge > 8*24*time.Hour {
		return false, fmt.Sprintf("FAIL — restore verification is stale (%s old)", durationAge(verificationAge))
	}
	return true, fmt.Sprintf("healthy; backup %s old, verified restore %s old", durationAge(backupAge), durationAge(verificationAge))
}

func backupRestoreProtection(status backupDoctorStatus, now time.Time) (bool, string) {
	if ok, detail := backupDoctorHealth(status, now); !ok {
		return false, detail
	}
	if status.LastBackupSet == "" || status.Verification.BackupSet != status.LastBackupSet {
		return false, "latest backup set has not passed restore verification"
	}
	if status.Verification.Status != "succeeded" {
		return false, "latest backup restore verification did not succeed"
	}
	if status.Verification.ConfigSnapshot.Status != "decrypted_and_checksum_verified" {
		return false, "latest backup config snapshot was not decrypted and checksum-verified"
	}
	stanzas := make(map[string]bool, len(status.Verification.Databases))
	for _, database := range status.Verification.Databases {
		stanzas[database.Stanza] = true
	}
	if len(stanzas) != 2 || !stanzas["openneko-metadata"] || !stanzas["openneko-records"] {
		return false, "latest backup verification did not restore both database stanzas"
	}
	return true, "latest whole-deployment backup set is restore-verified"
}

func durationAge(value time.Duration) string {
	if value < 0 {
		value = 0
	}
	return value.Round(time.Minute).String()
}

func composeCommandOutput(ctx context.Context, project string, files []string, command ...string) ([]byte, error) {
	args := []string{"compose"}
	if project != "" {
		args = append(args, "-p", project)
	}
	for _, file := range files {
		args = append(args, "-f", file)
	}
	args = append(args, command...)
	output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("%s", detail)
	}
	return output, nil
}
