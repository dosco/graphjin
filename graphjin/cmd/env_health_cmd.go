package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// A healthcheck for an image with no shell.
//
// ko builds on distroless static: there is no sh and no curl inside, so a
// container HEALTHCHECK cannot shell out to probe the server. The binary is
// already in there, so the probe belongs in the binary. Exit 0 means ready and
// nothing else does — a 200 carrying some other status is a server that is up
// and not serving, which is precisely the case a healthcheck exists to catch.

func envHealthCmd() *cobra.Command {
	var (
		url     string
		timeout time.Duration
		asJSON  bool
	)
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Check that a served environment is ready, for use as a container healthcheck",
		Args:  cobra.NoArgs,
		// The exit code is the answer; a usage dump on failure would bury it.
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !cmd.Flags().Changed("url") {
				url = defaultEnvHealthURL(os.Getenv)
			}
			body, err := fetchEnvHealth(cmd.Context(), url, timeout)
			if err != nil {
				return err
			}
			if asJSON {
				fmt.Fprintln(cmd.OutOrStdout(), strings.TrimSpace(string(body)))
				return nil
			}
			var health envHealthResponse
			if err := json.Unmarshal(body, &health); err != nil {
				return fmt.Errorf("%s did not answer with an environment health document: %w", url, err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "ready — %d worlds, %d tasks, suite %s, reward %s\n",
				health.Workers, health.Tasks, health.Suite.Version, health.RewardVersion)
			return nil
		},
	}
	cmd.Flags().StringVar(&url, "url", defaultEnvHealthURL(os.Getenv),
		"environment to check (defaults to the port GJ_ENV_LISTEN names)")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Second, "how long to wait for an answer")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the health document instead of a summary")
	return cmd
}

// fetchEnvHealth returns the health document only when the server says it is
// ready.
func fetchEnvHealth(ctx context.Context, url string, timeout time.Duration) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	endpoint := strings.TrimSuffix(strings.TrimSpace(url), "/") + "/health"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%s is not answering: %w", endpoint, err)
	}
	defer response.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %d", endpoint, response.StatusCode)
	}
	var health struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &health); err != nil {
		return nil, fmt.Errorf("%s did not answer with an environment health document: %w", endpoint, err)
	}
	if health.Status != "ready" {
		return nil, fmt.Errorf("%s reports status %q", endpoint, health.Status)
	}
	return body, nil
}

// defaultEnvHealthURL points the probe at whatever this container was told to
// listen on.
//
// A HEALTHCHECK that has to repeat the port is a HEALTHCHECK that goes stale
// the first time somebody changes GJ_ENV_LISTEN. The host is always loopback:
// the probe runs inside the container, and 0.0.0.0 is an address to bind, not
// one to dial.
func defaultEnvHealthURL(getenv func(string) string) string {
	listen := strings.TrimSpace(getenv(envServeVariable("listen")))
	if listen == "" {
		return "http://127.0.0.1:8090"
	}
	_, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "http://127.0.0.1:8090"
	}
	// A port that is not a number would produce a URL that fails in a way
	// nobody could read; the default at least fails somewhere expected.
	if port = strings.TrimSpace(port); port == "" {
		return "http://127.0.0.1:8090"
	}
	if _, err := strconv.Atoi(port); err != nil {
		return "http://127.0.0.1:8090"
	}
	return "http://127.0.0.1:" + port
}
