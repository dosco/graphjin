package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/open-neko/neko/apps/openneko/internal/prompt"
	"github.com/spf13/cobra"
)

type packRequestOptions struct {
	inputs, secretRefs, bindings  []string
	sourceID, version, reviewHash string
	yes                           bool
}

func addPackRequestFlags(cmd *cobra.Command, opts *packRequestOptions) {
	cmd.Flags().StringArrayVar(&opts.inputs, "input", nil, "Declared configuration key=value (repeatable)")
	cmd.Flags().StringArrayVar(&opts.secretRefs, "secret-ref", nil, "Declared secret key=stored-key in the pack's secret section (repeatable)")
	cmd.Flags().StringArrayVar(&opts.bindings, "bind", nil, "Pack source key=existing read-only source name (repeatable)")
	cmd.Flags().StringVar(&opts.sourceID, "source-id", "", "Organization data source ID")
	cmd.Flags().StringVar(&opts.version, "version", "", "Exact uploaded pack version")
	cmd.Flags().StringVar(&opts.reviewHash, "review-hash", "", "Approval hash from pack review with the same configuration")
	cmd.Flags().BoolVar(&opts.yes, "yes", false, "Approve the displayed review and apply it without prompting")
}

func (opts packRequestOptions) used(cmd *cobra.Command) bool {
	for _, flag := range []string{"input", "secret-ref", "bind", "source-id", "version", "review-hash", "yes"} {
		if cmd.Flags().Changed(flag) {
			return true
		}
	}
	return false
}

func packPairs(values []string) (map[string]string, error) {
	result := map[string]string{}
	for _, value := range values {
		key, content, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("expected key=value")
		}
		if _, exists := result[key]; exists {
			return nil, fmt.Errorf("duplicate configuration key %q", key)
		}
		result[key] = content
	}
	return result, nil
}

func newPackReviewCmd() *cobra.Command {
	var options packRequestOptions
	var operation, output string
	cmd := &cobra.Command{Use: "review <pack>", Short: "Review exact pack content and configuration before approval", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if code, proxied := MaybeProxyToWorker(cmd); proxied {
				return WithExit(code, nil)
			}
			if operation != "install" && operation != "configure" && operation != "upgrade" {
				return fmt.Errorf("--operation must be install, configure, or upgrade")
			}
			return runReviewedPack(cmd, args[0], operation, options, "review")
		}}
	cmd.Flags().StringVar(&operation, "operation", "install", "Operation to review: install, configure, or upgrade")
	addPackRequestFlags(cmd, &options)
	addPackOutputFlag(cmd, &output)
	return cmd
}

func runReviewedPack(cmd *cobra.Command, packID, operation string, opts packRequestOptions, output string) error {
	for _, legacy := range []string{"base-url", "store-code", "table-prefix", "database-connectivity", "database-host", "database-port", "database-name", "analytics-username-ref", "analytics-password-ref", "integration-token-ref"} {
		if cmd.Flags().Changed(legacy) {
			return fmt.Errorf("use --input and --secret-ref for reviewed configuration; do not combine them with --%s", legacy)
		}
	}
	path := "/admin/packs/" + url.PathEscape(packID)
	if operation == "configure" && opts.version == "" {
		var status packStatusResponse
		if _, err := requestPackAPI(cmd.Context(), http.MethodGet, path+"/status", nil, &status); err != nil {
			return err
		}
		opts.version = status.Version
	}
	var inspect struct {
		Manifest struct {
			Metadata struct {
				Version string `json:"version"`
			} `json:"metadata"`
			Inputs []struct {
				Key    string `json:"key"`
				Type   string `json:"type"`
				Values []any  `json:"values"`
			} `json:"inputs"`
		} `json:"manifest"`
	}
	query := ""
	if opts.version != "" {
		query = "?version=" + url.QueryEscape(opts.version)
	}
	if _, err := requestPackAPI(cmd.Context(), http.MethodGet, path+query, nil, &inspect); err != nil {
		return err
	}
	values, err := packPairs(opts.inputs)
	if err != nil {
		return err
	}
	refs, err := packPairs(opts.secretRefs)
	if err != nil {
		return err
	}
	bindings, err := packPairs(opts.bindings)
	if err != nil {
		return err
	}
	inputs := map[string]any{}
	for _, input := range inspect.Manifest.Inputs {
		value, exists := values[input.Key]
		if !exists {
			continue
		}
		switch input.Type {
		case "integer":
			number, err := strconv.Atoi(value)
			if err != nil {
				return fmt.Errorf("%s must be an integer", input.Key)
			}
			inputs[input.Key] = number
		case "boolean":
			flag, err := strconv.ParseBool(value)
			if err != nil {
				return fmt.Errorf("%s must be true or false", input.Key)
			}
			inputs[input.Key] = flag
		case "enum":
			found := false
			for _, option := range input.Values {
				if fmt.Sprint(option) == value {
					inputs[input.Key] = option
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("%s is not a declared choice", input.Key)
			}
		default:
			inputs[input.Key] = value
		}
		delete(values, input.Key)
	}
	if len(values) != 0 {
		return fmt.Errorf("configuration contains an input not declared by this pack")
	}
	body := map[string]any{"version": inspect.Manifest.Metadata.Version, "operation": operation, "inputs": inputs, "secretRefs": refs, "sourceBindings": bindings}
	if opts.sourceID != "" {
		body["dataSourceId"] = opts.sourceID
	}
	if opts.reviewHash == "" || output == "review" {
		var review struct {
			ReviewHash string `json:"reviewHash"`
		}
		raw, err := requestPackAPI(cmd.Context(), http.MethodPost, path+"/review", body, &review)
		if err != nil {
			return err
		}
		if review.ReviewHash == "" {
			return fmt.Errorf("pack service returned no review hash")
		}
		if output == "review" {
			_, err = cmd.OutOrStdout().Write(append(raw, '\n'))
			return err
		}
		fmt.Fprintln(cmd.ErrOrStderr(), string(raw))
		opts.reviewHash = review.ReviewHash
		approved := opts.yes
		nonInteractive, _ := cmd.Flags().GetBool("non-interactive")
		if !approved && !nonInteractive && prompt.IsInteractive() {
			answer, err := prompt.Visible("Apply this reviewed pack? [y/N] ")
			if err != nil {
				return err
			}
			approved = strings.EqualFold(strings.TrimSpace(answer), "y") || strings.EqualFold(strings.TrimSpace(answer), "yes")
		}
		if !approved {
			return fmt.Errorf("pack was not applied; review the result and rerun with --review-hash %s or --yes", review.ReviewHash)
		}
	}
	body["reviewHash"], body["idempotencyKey"] = opts.reviewHash, uuid.NewString()
	var result packStatusResponse
	raw, err := requestPackAPI(cmd.Context(), http.MethodPost, path+"/"+operation, body, &result)
	if err != nil {
		return err
	}
	if output == "json" {
		_, err = cmd.OutOrStdout().Write(append(raw, '\n'))
		return err
	}
	writePackStatus(cmd.OutOrStdout(), result)
	return nil
}

func newPackUploadCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{Use: "upload <archive.zip|->", Short: "Upload a pack for review without installing it", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var reader io.Reader = cmd.InOrStdin()
			if args[0] != "-" {
				file, err := os.Open(args[0])
				if err != nil {
					return err
				}
				defer file.Close()
				reader = file
			}
			archive, err := io.ReadAll(io.LimitReader(reader, (16<<20)+1))
			if err != nil {
				return err
			}
			if len(archive) == 0 || len(archive) > 16<<20 {
				return fmt.Errorf("choose a non-empty ZIP of at most 16 MiB")
			}
			var raw []byte
			// Read the host file here and stream it to the selected worker. A host
			// path is not meaningful inside the worker's container filesystem.
			code, proxied := MaybeProxyToWorker(cmd, func(container string) int {
				process := exec.CommandContext(cmd.Context(), "docker", "exec", "-i", container, "curl", "--silent", "--show-error", "--fail-with-body", "--max-time", "120", "-H", "Content-Type: application/zip", "--data-binary", "@-", defaultWorkerAdminURL+"/admin/packs/upload")
				process.Stdin, process.Stderr = bytes.NewReader(archive), cmd.ErrOrStderr()
				raw, err = process.Output()
				if err != nil {
					return 1
				}
				return 0
			})
			if proxied && code != 0 {
				return fmt.Errorf("pack upload failed: %s", strings.TrimSpace(string(raw)))
			}
			if !proxied {
				raw, err = requestPackAPI(cmd.Context(), http.MethodPost, "/admin/packs/upload", archive, nil)
				if err != nil {
					return err
				}
			}
			if output == "json" {
				_, err = cmd.OutOrStdout().Write(append(raw, '\n'))
				return err
			}
			var result struct {
				PackID  string `json:"packId"`
				Version string `json:"version"`
			}
			if err := json.Unmarshal(raw, &result); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Uploaded %s@%s for review. Nothing was installed.\n", terminalSafe(result.PackID), terminalSafe(result.Version))
			return nil
		}}
	addPackOutputFlag(cmd, &output)
	return cmd
}

// terminalSafe replaces control characters in server-supplied text before it
// is printed, so a response cannot inject terminal escape sequences.
func terminalSafe(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '�'
		}
		return r
	}, value)
}
