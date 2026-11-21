package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
	"github.com/spf13/cobra"
)

// evalMinedPath is where tests mined from real questions are kept. They are a
// suite of their own: the generated suite samples its candidates, and a
// question somebody really asked should never lose that draw.
func evalMinedPath(projectPath string) string {
	return filepath.Join(projectPath, gjeval.DefaultEvaluationDir, "mined.yml")
}

// experienceDocument is what GET /api/v1/agent/experience returns.
type experienceDocument struct {
	Cases   []gjeval.MinedCase `json:"cases"`
	Summary gjeval.MineSummary `json:"summary"`
	Page    struct {
		RunsRead   int    `json:"runs_read"`
		NextOffset *int   `json:"next_offset"`
		Until      string `json:"until"`
	} `json:"page"`
}

func evalMineCmd(opts *evalCLIOptions) *cobra.Command {
	var (
		from, serverURL, token, since, origin, out string
		headers                                    []string
		maxRuns, concurrency                       int
	)
	command := &cobra.Command{
		Use:   "mine",
		Short: "Turn questions people asked the agent into tests",
		Long: `Turn questions people asked the agent into tests.

A running server keeps every thread turn: the questions the lead asked the data
agent, the answers, and the queries that produced them. GET
/api/v1/agent/experience (admin only) keeps the questions whose query computes
one figure in the database, re-runs each query as the person who asked, and
drops any whose answer no longer passes it. This command fetches those cases
(or reads them from --from), verifies every answer key against the evaluation
target like any other task, and writes them to eval/mined.yml.

Run them with: deeporg eval run --suite eval/mined.yml`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			projectPath, target, err := resolveEvalTarget(cmd, opts)
			if err != nil {
				return err
			}
			var doc experienceDocument
			source := from
			if strings.TrimSpace(from) != "" {
				doc, err = readExperienceFile(from)
			} else {
				base, auth, resolveErr := experienceServer(serverURL, token, opts.Remote)
				if resolveErr != nil {
					return resolveErr
				}
				for _, h := range headers {
					name, value, ok := strings.Cut(h, "=")
					if !ok {
						name, value, ok = strings.Cut(h, ":")
					}
					if !ok || strings.TrimSpace(name) == "" {
						return fmt.Errorf("--header %q: want name=value", h)
					}
					auth[strings.TrimSpace(name)] = strings.TrimSpace(value)
				}
				source = base
				doc, err = fetchExperience(cmd.Context(), base, auth, since, origin, maxRuns)
			}
			if err != nil {
				return err
			}

			frozenClock, err := evalFrozenClock(opts)
			if err != nil {
				return err
			}
			instance, err := (evalEnvironment{StatusOut: os.Stderr}).Start(cmd.Context(), gjeval.EnvSpec{Target: target, ConfigPath: projectPath, FreezeTime: opts.FreezeTime})
			if err != nil {
				return evalEnvironmentError(err)
			}
			defer instance.Close() //nolint:errcheck
			client := &http.Client{Timeout: 120 * time.Second}
			verifier := &gjeval.Verifier{Client: client, BaseURL: instance.BaseURL(), Headers: instance.Headers(), Now: frozenClock}
			suite, skipped, err := gjeval.MinedSuite(doc.Cases, instance.Fingerprint().CatalogHash, time.Now())
			if err != nil {
				return err
			}
			// Nothing is trusted because of where it came from: an answer key that
			// does not resolve against the target is not a test there.
			verified, err := (gjeval.Generator{Verifier: verifier}).VerifyTasks(cmd.Context(), suite.Tasks, 0, concurrency)
			if err != nil {
				return &evalExitError{Code: 2, Err: err}
			}
			suite.Tasks = verified
			path := evalMinedPath(projectPath)
			if strings.TrimSpace(out) != "" {
				path = out
			}
			tiers := map[string]int{}
			for _, task := range verified {
				tiers[strings.TrimPrefix(task.Provenance.Source, gjeval.MinedSourcePrefix)]++
			}
			if len(verified) != 0 {
				if err := gjeval.SaveSuite(path, suite); err != nil {
					return err
				}
			}
			if opts.JSON {
				return writeEvalJSON(cmd.OutOrStdout(), map[string]any{
					"status": "mined", "source": source, "suite": path, "written": len(verified) != 0, "tests": len(verified),
					"by_tier": tiers, "server_summary": doc.Summary, "not_verified": len(doc.Cases) - len(verified),
					"skipped": skipped,
				})
			}
			w := cmd.OutOrStdout()
			s := doc.Summary
			fmt.Fprintf(w, "Read %d questions to the data agent from %s.\n", s.Candidates, source)
			fmt.Fprintf(w, "  %d became tests on the server", s.Tests)
			if len(s.ByTier) != 0 {
				fmt.Fprintf(w, " (%s)", formatCounts(s.ByTier))
			}
			fmt.Fprintln(w, ".")
			if len(s.Dropped) != 0 {
				fmt.Fprintf(w, "  not tests yet: %s\n", formatCounts(s.Dropped))
			}
			if dropped := len(doc.Cases) - len(verified); dropped != 0 {
				fmt.Fprintf(w, "  %d did not verify against %s and were left out.\n", dropped, instance.Label())
			}
			for _, line := range skipped {
				fmt.Fprintf(w, "  skipped %s\n", line)
			}
			if len(verified) == 0 {
				// An earlier suite stays: an empty mine is no reason to lose it,
				// but a run of it is a run of the old tests.
				if _, statErr := os.Stat(path); statErr == nil {
					fmt.Fprintf(w, "No tests to write; %s still holds the tests from an earlier mine.\n", path)
				} else {
					fmt.Fprintln(w, "No tests to write.")
				}
				return nil
			}
			fmt.Fprintf(w, "Wrote %d tests to %s (%s).\n", len(verified), path, formatCounts(tiers))
			fmt.Fprintf(w, "Run them with: deeporg eval run --suite %s\n", evalMinedRunPath(projectPath, path))
			return nil
		},
	}
	command.Flags().StringVar(&from, "from", "", "read cases saved from /api/v1/agent/experience instead of fetching them")
	command.Flags().StringVar(&serverURL, "url", "", "server to read experience from (default: the --remote server)")
	command.Flags().StringVar(&token, "token", "", "bearer token for an admin (default: $GRAPHJIN_EVAL_TOKEN, then the cli setup token)")
	command.Flags().StringSliceVar(&headers, "header", nil, "extra request header name=value, repeatable")
	command.Flags().StringVar(&since, "since", "", "only turns at or after this RFC 3339 time")
	command.Flags().StringVar(&origin, "origin", "", "only turns of this kind: chat, routine, continuation or outcome")
	command.Flags().IntVar(&maxRuns, "max-runs", 1000, "stop after reading this many runs")
	command.Flags().IntVar(&concurrency, "verify-concurrency", 4, "how many answer keys to verify at once")
	command.Flags().StringVar(&out, "out", "", "write the suite here instead of eval/mined.yml")
	return command
}

// evalMinedRunPath is the suite path to print: relative to the project when it
// is inside it, which is how eval run resolves a relative --suite.
func evalMinedRunPath(projectPath, path string) string {
	if rel, err := filepath.Rel(projectPath, path); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return path
}

func formatCounts(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		return keys[i] < keys[j]
	})
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%d %s", counts[key], key))
	}
	return strings.Join(parts, ", ")
}

// experienceServer is where to read experience from, with the credentials to
// read it.
func experienceServer(serverURL, token string, remote bool) (string, map[string]string, error) {
	headers := map[string]string{}
	if token == "" {
		token = strings.TrimSpace(os.Getenv("GRAPHJIN_EVAL_TOKEN"))
	}
	if serverURL == "" || token == "" {
		clientConfig, err := LoadClientConfig()
		if err == nil && clientConfig != nil {
			if serverURL == "" && remote {
				serverURL = strings.TrimSpace(clientConfig.Server)
			}
			if token == "" {
				token = strings.TrimSpace(clientConfig.Token)
			}
		}
	}
	if serverURL == "" {
		return "", nil, errors.New("say where to read experience from: --url, --remote (the cli setup server) or --from a saved file")
	}
	if token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	return strings.TrimRight(serverURL, "/"), headers, nil
}

// fetchExperience reads every page of experience the server has, up to
// maxRuns runs.
func fetchExperience(ctx context.Context, base string, headers map[string]string, since, origin string, maxRuns int) (experienceDocument, error) {
	var all experienceDocument
	all.Summary = gjeval.MineSummary{ByTier: map[string]int{}, Dropped: map[string]int{}}
	client := &http.Client{Timeout: 10 * time.Minute}
	offset, read, until := 0, 0, ""
	for read < maxRuns {
		q := url.Values{}
		q.Set("offset", strconv.Itoa(offset))
		q.Set("limit", strconv.Itoa(min(200, maxRuns-read)))
		if since != "" {
			q.Set("since", since)
		}
		if origin != "" {
			q.Set("origin", origin)
		}
		// Later pages list the runs the first page saw, whatever finished since.
		if until != "" {
			q.Set("until", until)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/agent/experience?"+q.Encode(), nil)
		if err != nil {
			return all, err
		}
		for name, value := range headers {
			req.Header.Set(name, value)
		}
		resp, err := client.Do(req)
		if err != nil {
			return all, err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close() //nolint:errcheck
		if err != nil {
			return all, err
		}
		if resp.StatusCode != http.StatusOK {
			return all, fmt.Errorf("GET %s/api/v1/agent/experience: %s: %s", base, resp.Status, strings.TrimSpace(string(body)))
		}
		var page experienceDocument
		if err := json.Unmarshal(body, &page); err != nil {
			return all, fmt.Errorf("decode experience: %w", err)
		}
		mergeExperience(&all, page)
		read += page.Page.RunsRead
		if page.Page.NextOffset == nil || page.Page.RunsRead == 0 {
			break
		}
		offset, until = *page.Page.NextOffset, page.Page.Until
	}
	all.Page.RunsRead = read
	return all, nil
}

// mergeExperience adds one page. Each page keeps one test per question, so a
// question asked on two pages is kept once, the newer one, which the server
// lists first.
func mergeExperience(all *experienceDocument, page experienceDocument) {
	at := map[string]int{}
	for i, c := range all.Cases {
		at[strings.ToLower(strings.Join(strings.Fields(c.Prompt), " "))] = i
	}
	for _, c := range page.Cases {
		key := strings.ToLower(strings.Join(strings.Fields(c.Prompt), " "))
		i, seen := at[key]
		if !seen {
			at[key] = len(all.Cases)
			all.Cases = append(all.Cases, c)
			continue
		}
		// The same question on two pages keeps the one the server would have
		// kept: a confirmed test is never lost to an incumbent copy.
		all.Summary.Dropped[gjeval.MineDropDuplicate]++
		if gjeval.MinedCaseOutranks(c, all.Cases[i]) {
			all.Cases[i] = c
		}
	}
	all.Summary.Candidates += page.Summary.Candidates
	for reason, n := range page.Summary.Dropped {
		all.Summary.Dropped[reason] += n
	}
	all.Summary.ByTier = map[string]int{}
	for _, c := range all.Cases {
		all.Summary.ByTier[c.Tier]++
	}
	all.Summary.Tests = len(all.Cases)
}

// readExperienceFile reads a saved /api/v1/agent/experience response, or a bare
// list of cases.
func readExperienceFile(path string) (experienceDocument, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return experienceDocument{}, err
	}
	var doc experienceDocument
	if err := json.Unmarshal(raw, &doc); err == nil && (doc.Cases != nil || doc.Summary.Candidates != 0) {
		if doc.Summary.ByTier == nil {
			doc.Summary.ByTier = map[string]int{}
		}
		return doc, nil
	}
	var cases []gjeval.MinedCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		return experienceDocument{}, fmt.Errorf("%s is neither an experience response nor a list of cases: %w", path, err)
	}
	doc = experienceDocument{Cases: cases, Summary: gjeval.MineSummary{Candidates: len(cases), Tests: len(cases), ByTier: map[string]int{}}}
	for _, c := range cases {
		doc.Summary.ByTier[c.Tier]++
	}
	return doc, nil
}
