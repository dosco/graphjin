package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/open-neko/neko/apps/openneko/internal/prompt"
)

const defaultWorkerAdminURL = "http://127.0.0.1:4100"

type packListEntry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Installed bool   `json:"installed"`
}

type packStatusResponse struct {
	PackID    string `json:"packId"`
	Version   string `json:"version"`
	Status    string `json:"status"`
	Readiness map[string]struct {
		Status string  `json:"status"`
		Reason *string `json:"reason"`
	} `json:"readiness"`
	InstalledAt *string `json:"installedAt"`
	LastError   *string `json:"lastError"`
}

type packDoctorResponse struct {
	PackID string `json:"packId"`
	Status string `json:"status"`
	Checks []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Detail string `json:"detail"`
	} `json:"checks"`
}

type packInstallOptions struct {
	baseURL          string
	storeCode        string
	tablePrefix      string
	databaseMode     string
	databaseHost     string
	databasePort     int
	databaseName     string
	analyticsUserRef string
	analyticsPassRef string
	integrationRef   string
	nonInteractive   bool
	output           string
}

func newPackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pack",
		Short: "Install and manage application-specific solution packs",
	}
	cmd.AddCommand(
		newPackUploadCmd(),
		newPackReviewCmd(),
		newPackListCmd(),
		newPackInspectCmd(),
		newPackPlanCmd(),
		newPackInstallCmd(),
		newPackStatusCmd(),
		newPackManageCmd(),
		newPackDoctorCmd(),
		newPackConfigureCmd(),
		newPackUpgradeCmd(),
		newPackUninstallCmd(),
	)
	return cmd
}

func newPackManageCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:     "manage <pack>",
		Aliases: []string{"overview"},
		Short:   "Show pack health and common administration commands",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if code, proxied := MaybeProxyToWorker(cmd); proxied {
				return WithExit(code, nil)
			}
			packID := args[0]
			var status packStatusResponse
			statusRaw, err := requestPackAPI(cmd.Context(), http.MethodGet, "/admin/packs/"+url.PathEscape(packID)+"/status", nil, &status)
			if err != nil {
				return err
			}
			var doctor packDoctorResponse
			doctorRaw, err := requestPackAPI(cmd.Context(), http.MethodGet, "/admin/packs/"+url.PathEscape(packID)+"/doctor", nil, &doctor)
			if err != nil {
				return err
			}
			if output == "json" {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]json.RawMessage{
					"status": statusRaw,
					"doctor": doctorRaw,
				})
			}
			writePackStatus(cmd.OutOrStdout(), status)
			fmt.Fprintln(cmd.OutOrStdout(), "Health checks:")
			for _, check := range doctor.Checks {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s: %s — %s\n", packCapabilityLabel(check.ID), check.Status, check.Detail)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Common tasks:")
			fmt.Fprintf(cmd.OutOrStdout(), "  Recheck connection: openneko pack doctor %s\n", packID)
			fmt.Fprintf(cmd.OutOrStdout(), "  Update credentials: openneko pack configure %s\n", packID)
			fmt.Fprintf(cmd.OutOrStdout(), "  Apply an update:    openneko pack upgrade %s\n", packID)
			fmt.Fprintf(cmd.OutOrStdout(), "  Remove safely:      openneko pack uninstall %s\n", packID)
			return nil
		},
	}
	addPackOutputFlag(cmd, &output)
	return cmd
}

func newPackDoctorCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "doctor <pack>",
		Short: "Recheck pack connectivity, permissions, and capability readiness",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if code, proxied := MaybeProxyToWorker(cmd); proxied {
				return WithExit(code, nil)
			}
			var response packDoctorResponse
			raw, err := requestPackAPI(cmd.Context(), http.MethodGet, "/admin/packs/"+url.PathEscape(args[0])+"/doctor", nil, &response)
			if err != nil {
				return err
			}
			if output == "json" {
				_, err = cmd.OutOrStdout().Write(append(raw, '\n'))
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", response.PackID, response.Status)
			for _, check := range response.Checks {
				fmt.Fprintf(cmd.OutOrStdout(), "  %s: %s — %s\n", packCapabilityLabel(check.ID), check.Status, check.Detail)
			}
			return nil
		},
	}
	addPackOutputFlag(cmd, &output)
	return cmd
}

func newPackConfigureCmd() *cobra.Command {
	var generic packRequestOptions
	var analyticsUserRef string
	var analyticsPassRef string
	var integrationRef string
	var output string
	cmd := &cobra.Command{
		Use:   "configure <pack>",
		Short: "Update pack configuration or credentials and re-run readiness checks",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if code, proxied := MaybeProxyToWorker(cmd); proxied {
				return WithExit(code, nil)
			}
			if args[0] != "magento" || generic.used(cmd) {
				return runReviewedPack(cmd, args[0], "configure", generic, output)
			}
			secretRefs := map[string]string{}
			for key, value := range map[string]string{
				"database.analytics_username": analyticsUserRef,
				"database.analytics_password": analyticsPassRef,
				"magento.integration_token":   integrationRef,
			} {
				if value != "" {
					secretRefs[key] = value
				}
			}
			body := map[string]any{
				"secretRefs":     secretRefs,
				"idempotencyKey": uuid.NewString(),
			}
			var response packStatusResponse
			raw, err := requestPackAPI(cmd.Context(), http.MethodPost, "/admin/packs/"+url.PathEscape(args[0])+"/configure", body, &response)
			if err != nil {
				return err
			}
			if output == "json" {
				_, err = cmd.OutOrStdout().Write(append(raw, '\n'))
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "configured %s@%s\n", response.PackID, response.Version)
			writePackReadiness(cmd.OutOrStdout(), response.Readiness)
			return nil
		},
	}
	cmd.Flags().StringVar(&analyticsUserRef, "analytics-username-ref", "", "Stored key in the pack.magento secret section")
	cmd.Flags().StringVar(&analyticsPassRef, "analytics-password-ref", "", "Stored key in the pack.magento secret section")
	cmd.Flags().StringVar(&integrationRef, "integration-token-ref", "", "Stored Magento integration token key")
	addPackRequestFlags(cmd, &generic)
	addPackOutputFlag(cmd, &output)
	return cmd
}

func newPackUpgradeCmd() *cobra.Command {
	var generic packRequestOptions
	var output string
	cmd := &cobra.Command{
		Use:   "upgrade <pack>",
		Short: "Apply an available pack version while preserving operator changes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if code, proxied := MaybeProxyToWorker(cmd); proxied {
				return WithExit(code, nil)
			}
			body := map[string]any{"idempotencyKey": uuid.NewString()}
			if args[0] != "magento" || generic.used(cmd) {
				return runReviewedPack(cmd, args[0], "upgrade", generic, output)
			}
			var response packStatusResponse
			raw, err := requestPackAPI(cmd.Context(), http.MethodPost, "/admin/packs/"+url.PathEscape(args[0])+"/upgrade", body, &response)
			if err != nil {
				return err
			}
			if output == "json" {
				_, err = cmd.OutOrStdout().Write(append(raw, '\n'))
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "upgraded %s@%s\n", response.PackID, response.Version)
			writePackReadiness(cmd.OutOrStdout(), response.Readiness)
			return nil
		},
	}
	addPackRequestFlags(cmd, &generic)
	addPackOutputFlag(cmd, &output)
	return cmd
}

func newPackUninstallCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "uninstall <pack>",
		Short: "Safely remove unchanged pack configuration while retaining history and data",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if code, proxied := MaybeProxyToWorker(cmd); proxied {
				return WithExit(code, nil)
			}
			body := map[string]any{"idempotencyKey": uuid.NewString()}
			var response packStatusResponse
			raw, err := requestPackAPI(cmd.Context(), http.MethodPost, "/admin/packs/"+url.PathEscape(args[0])+"/uninstall", body, &response)
			if err != nil {
				return err
			}
			if output == "json" {
				_, err = cmd.OutOrStdout().Write(append(raw, '\n'))
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "uninstalled %s@%s (history and data retained)\n", response.PackID, response.Version)
			return nil
		},
	}
	addPackOutputFlag(cmd, &output)
	return cmd
}

func newPackListCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List first-party and uploaded solution packs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if code, proxied := MaybeProxyToWorker(cmd); proxied {
				return WithExit(code, nil)
			}
			var response struct {
				Packs []packListEntry `json:"packs"`
			}
			raw, err := requestPackAPI(cmd.Context(), http.MethodGet, "/admin/packs", nil, &response)
			if err != nil {
				return err
			}
			if output == "json" {
				_, err = cmd.OutOrStdout().Write(append(raw, '\n'))
				return err
			}
			if len(response.Packs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no solution packs are available")
				return nil
			}
			for _, entry := range response.Packs {
				state := "available"
				if entry.Installed {
					state = "installed"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", entry.ID, entry.Name, entry.Version, state)
			}
			return nil
		},
	}
	addPackOutputFlag(cmd, &output)
	return cmd
}

func newPackInspectCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "inspect <pack>",
		Short: "Show a pack's metadata, requirements, and permissions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if code, proxied := MaybeProxyToWorker(cmd); proxied {
				return WithExit(code, nil)
			}
			path := "/admin/packs/" + url.PathEscape(args[0])
			var response struct {
				Manifest struct {
					Metadata struct {
						ID        string `json:"id"`
						Name      string `json:"name"`
						Version   string `json:"version"`
						Publisher string `json:"publisher"`
						Category  string `json:"category"`
					} `json:"metadata"`
					Inputs []struct {
						Key      string `json:"key"`
						Type     string `json:"type"`
						Required bool   `json:"required"`
						Discover bool   `json:"discover"`
					} `json:"inputs"`
				} `json:"manifest"`
				Permissions map[string]string `json:"permissions"`
			}
			raw, err := requestPackAPI(cmd.Context(), http.MethodGet, path, nil, &response)
			if err != nil {
				return err
			}
			if output == "json" {
				_, err = cmd.OutOrStdout().Write(append(raw, '\n'))
				return err
			}
			meta := response.Manifest.Metadata
			fmt.Fprintf(cmd.OutOrStdout(), "%s (%s)\nVersion: %s\nPublisher: %s\nCategory: %s\n", meta.Name, meta.ID, meta.Version, meta.Publisher, meta.Category)
			if len(response.Permissions) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Permissions:")
				keys := make([]string, 0, len(response.Permissions))
				for key := range response.Permissions {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				for _, key := range keys {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s: %s\n", key, response.Permissions[key])
				}
			}
			var required []string
			for _, input := range response.Manifest.Inputs {
				if input.Required && !input.Discover {
					required = append(required, input.Key)
				}
			}
			if len(required) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Required configuration: %s\n", strings.Join(required, ", "))
			}
			return nil
		},
	}
	addPackOutputFlag(cmd, &output)
	return cmd
}

func newPackPlanCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "plan <pack>",
		Short: "Preview the artifact changes for a pack install or upgrade",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if code, proxied := MaybeProxyToWorker(cmd); proxied {
				return WithExit(code, nil)
			}
			var response struct {
				PackID  string `json:"packId"`
				Version string `json:"version"`
				Hash    string `json:"hash"`
				Entries []struct {
					Action    string `json:"action"`
					Kind      string `json:"kind"`
					Key       string `json:"key"`
					TargetRef string `json:"targetRef"`
					Reason    string `json:"reason"`
				} `json:"entries"`
			}
			raw, err := requestPackAPI(cmd.Context(), http.MethodGet, "/admin/packs/"+url.PathEscape(args[0])+"/plan", nil, &response)
			if err != nil {
				return err
			}
			if output == "json" {
				_, err = cmd.OutOrStdout().Write(append(raw, '\n'))
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", response.PackID, response.Version)
			for _, entry := range response.Entries {
				suffix := ""
				if entry.Reason != "" {
					suffix = " (" + entry.Reason + ")"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "  %-8s %-14s %s%s\n", entry.Action, entry.Kind, entry.TargetRef, suffix)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Plan: %s\n", response.Hash)
			return nil
		},
	}
	addPackOutputFlag(cmd, &output)
	return cmd
}

func newPackStatusCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:   "status <pack>",
		Short: "Show pack installation and capability readiness",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if code, proxied := MaybeProxyToWorker(cmd); proxied {
				return WithExit(code, nil)
			}
			var response packStatusResponse
			raw, err := requestPackAPI(cmd.Context(), http.MethodGet, "/admin/packs/"+url.PathEscape(args[0])+"/status", nil, &response)
			if err != nil {
				return err
			}
			if output == "json" {
				_, err = cmd.OutOrStdout().Write(append(raw, '\n'))
				return err
			}
			writePackStatus(cmd.OutOrStdout(), response)
			return nil
		},
	}
	addPackOutputFlag(cmd, &output)
	return cmd
}

func newPackInstallCmd() *cobra.Command {
	var generic packRequestOptions
	opts := packInstallOptions{storeCode: "all", databaseMode: "external_network", databasePort: 3306, output: "text"}
	cmd := &cobra.Command{
		Use:   "install <pack>",
		Short: "Install every artifact in a solution pack",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if code, proxied := MaybeProxyToWorker(cmd); proxied {
				return WithExit(code, nil)
			}
			if args[0] != "magento" || generic.used(cmd) {
				return runReviewedPack(cmd, args[0], "install", generic, opts.output)
			}
			if err := completeMagentoInstallOptions(&opts); err != nil {
				return err
			}
			secretRefs := map[string]string{}
			for key, value := range map[string]string{
				"database.analytics_username": opts.analyticsUserRef,
				"database.analytics_password": opts.analyticsPassRef,
				"magento.integration_token":   opts.integrationRef,
			} {
				if value != "" {
					secretRefs[key] = value
				}
			}
			secrets := map[string]string{}
			if opts.analyticsUserRef == "" || opts.analyticsPassRef == "" {
				if opts.nonInteractive || !prompt.IsInteractive() {
					return errors.New("pack install: analytics credentials are required; use --analytics-username-ref and --analytics-password-ref with keys stored under the pack.magento secret section")
				}
				if opts.analyticsUserRef == "" {
					value, err := prompt.Visible("Read-only analytics username: ")
					if err != nil {
						return err
					}
					secrets["database.analytics_username"] = value
				}
				if opts.analyticsPassRef == "" {
					value, err := prompt.Hidden("Read-only analytics password (hidden): ")
					if err != nil {
						return err
					}
					secrets["database.analytics_password"] = value
				}
			}
			body := map[string]any{
				"inputs": map[string]any{
					"magento.base_url":           opts.baseURL,
					"magento.store_code":         opts.storeCode,
					"magento.table_prefix":       opts.tablePrefix,
					"database.connectivity_mode": opts.databaseMode,
					"database.host":              opts.databaseHost,
					"database.port":              opts.databasePort,
					"database.name":              opts.databaseName,
				},
				"secrets":        secrets,
				"secretRefs":     secretRefs,
				"idempotencyKey": uuid.NewString(),
			}
			var response packStatusResponse
			raw, err := requestPackAPI(cmd.Context(), http.MethodPost, "/admin/packs/"+url.PathEscape(args[0])+"/install", body, &response)
			if err != nil {
				return err
			}
			if opts.output == "json" {
				_, err = cmd.OutOrStdout().Write(append(raw, '\n'))
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "installed %s@%s\n", response.PackID, response.Version)
			writePackReadiness(cmd.OutOrStdout(), response.Readiness)
			fmt.Fprintln(cmd.OutOrStdout(), "Credentials were saved in OpenNeko's local secret store.")
			fmt.Fprintf(cmd.OutOrStdout(), "Manage this pack: openneko pack manage %s\n", response.PackID)
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.baseURL, "base-url", "", "Magento base URL (for example https://store.example.com)")
	cmd.Flags().StringVar(&opts.storeCode, "store-code", opts.storeCode, "Magento REST store code")
	cmd.Flags().StringVar(&opts.tablePrefix, "table-prefix", "", "Magento database table prefix (discovered when omitted)")
	cmd.Flags().StringVar(&opts.databaseMode, "database-connectivity", opts.databaseMode, "Database connectivity: external_network, host_gateway, or remote")
	cmd.Flags().StringVar(&opts.databaseHost, "database-host", "", "MariaDB/MySQL host reachable from OpenNeko")
	cmd.Flags().IntVar(&opts.databasePort, "database-port", opts.databasePort, "MariaDB/MySQL port")
	cmd.Flags().StringVar(&opts.databaseName, "database-name", "", "Magento database name")
	cmd.Flags().StringVar(&opts.analyticsUserRef, "analytics-username-ref", "", "Stored key in the pack.magento secret section")
	cmd.Flags().StringVar(&opts.analyticsPassRef, "analytics-password-ref", "", "Stored key in the pack.magento secret section")
	cmd.Flags().StringVar(&opts.integrationRef, "integration-token-ref", "", "Optional stored Magento integration token key")
	cmd.Flags().BoolVar(&opts.nonInteractive, "non-interactive", false, "Never prompt; fail if required configuration is missing")
	addPackRequestFlags(cmd, &generic)
	addPackOutputFlag(cmd, &opts.output)
	return cmd
}

func completeMagentoInstallOptions(opts *packInstallOptions) error {
	if opts.nonInteractive || !prompt.IsInteractive() {
		missing := make([]string, 0, 3)
		if strings.TrimSpace(opts.baseURL) == "" {
			missing = append(missing, "--base-url")
		}
		if strings.TrimSpace(opts.databaseHost) == "" {
			missing = append(missing, "--database-host")
		}
		if strings.TrimSpace(opts.databaseName) == "" {
			missing = append(missing, "--database-name")
		}
		if len(missing) > 0 {
			return fmt.Errorf("pack install: required flags missing in non-interactive mode: %s", strings.Join(missing, ", "))
		}
	} else {
		var err error
		fmt.Fprintln(os.Stdout, "Magento setup")
		fmt.Fprintln(os.Stdout, "Tip: for separate Docker/OrbStack stacks, publish MariaDB and use host.docker.internal.")
		if opts.baseURL == "" {
			opts.baseURL, err = prompt.Visible("Magento base URL: ")
			if err != nil {
				return err
			}
		}
		if opts.databaseHost == "" {
			opts.databaseHost, err = prompt.Visible("MariaDB/MySQL host: ")
			if err != nil {
				return err
			}
		}
		if opts.databaseName == "" {
			opts.databaseName, err = prompt.Visible("Magento database name: ")
			if err != nil {
				return err
			}
		}
	}
	for label, value := range map[string]string{
		"Magento base URL": opts.baseURL,
		"database host":    opts.databaseHost,
		"database name":    opts.databaseName,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("pack install: %s must not be empty", label)
		}
	}
	if parsed, err := url.ParseRequestURI(opts.baseURL); err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("pack install: --base-url must be an absolute HTTP or HTTPS URL")
	}
	if opts.databasePort < 1 || opts.databasePort > 65535 {
		return errors.New("pack install: --database-port must be between 1 and 65535")
	}
	if opts.databaseMode != "external_network" && opts.databaseMode != "host_gateway" && opts.databaseMode != "remote" {
		return errors.New("pack install: --database-connectivity must be external_network, host_gateway, or remote")
	}
	return nil
}

func addPackOutputFlag(cmd *cobra.Command, output *string) {
	cmd.Flags().StringVarP(output, "output", "o", "text", "Output format: text or json")
	cmd.PreRunE = func(_ *cobra.Command, _ []string) error {
		if *output != "text" && *output != "json" {
			return errors.New("--output must be text or json")
		}
		return nil
	}
}

func writePackStatus(out io.Writer, response packStatusResponse) {
	fmt.Fprintf(out, "%s@%s: %s\n", response.PackID, response.Version, response.Status)
	writePackReadiness(out, response.Readiness)
	if response.LastError != nil && *response.LastError != "" {
		fmt.Fprintf(out, "Last error: %s\n", *response.LastError)
	}
}

func writePackReadiness(out io.Writer, readiness map[string]struct {
	Status string  `json:"status"`
	Reason *string `json:"reason"`
}) {
	keys := make([]string, 0, len(readiness))
	for key := range readiness {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := readiness[key]
		status := value.Status
		if key == "operator" && status == "blocked" {
			status = "optional"
		}
		reason := ""
		if value.Reason != nil && *value.Reason != "" {
			reason = " — " + packReadinessReason(*value.Reason)
		}
		fmt.Fprintf(out, "  %s: %s%s\n", packCapabilityLabel(key), status, reason)
	}
}

func packCapabilityLabel(value string) string {
	if value == "operator" {
		return "write actions"
	}
	return value
}

func packReadinessReason(reason string) string {
	switch reason {
	case "integration_token_missing":
		return "add a Magento API token to enable them"
	case "integration_token_invalid":
		return "the Magento API token needs attention"
	case "acl_missing":
		return "the Magento API token needs more permissions"
	case "graphjin_version_unsupported":
		return "available after the required GraphJin update"
	default:
		return strings.ReplaceAll(reason, "_", " ")
	}
}

func requestPackAPI(ctx context.Context, method, path string, body any, destination any) ([]byte, error) {
	base := strings.TrimRight(os.Getenv("WORKER_ADMIN_URL"), "/")
	if base == "" {
		base = defaultWorkerAdminURL
	}
	endpoint := base + path
	var reader io.Reader
	if body != nil {
		if archive, ok := body.([]byte); ok {
			reader = bytes.NewReader(archive)
		} else {
			encoded, err := json.Marshal(body)
			if err != nil {
				return nil, err
			}
			reader = bytes.NewReader(encoded)
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		if _, ok := body.([]byte); ok {
			req.Header.Set("Content-Type", "application/zip")
		}
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pack service at %s is unreachable: %w", base, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var errorBody struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &errorBody)
		if errorBody.Error == "" {
			errorBody.Error = strings.TrimSpace(string(raw))
		}
		if errorBody.Error == "" {
			errorBody.Error = http.StatusText(res.StatusCode)
		}
		return nil, fmt.Errorf("pack service: %s (HTTP %s)", errorBody.Error, strconv.Itoa(res.StatusCode))
	}
	if destination != nil {
		if err := json.Unmarshal(raw, destination); err != nil {
			return nil, fmt.Errorf("pack service returned invalid JSON: %w", err)
		}
	}
	return raw, nil
}
