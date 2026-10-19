package serv

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/dosco/graphjin/auth/v3"
	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/featurecap"
)

const consoleBootstrapSchemaVersion = "1"

type consoleBootstrapIdentity struct {
	DisplayName   string `json:"display_name,omitempty"`
	Role          string `json:"role"`
	Authenticated bool   `json:"authenticated"`
	// Suggested is a development identity the console may adopt when the
	// browser has none. Advisory only: every authorization decision is still
	// made from the request's own credentials.
	Suggested *consoleSuggestedIdentity `json:"suggested,omitempty"`
}

type consoleSuggestedIdentity struct {
	UserID    string `json:"user_id"`
	Role      string `json:"role,omitempty"`
	AccountID string `json:"account_id,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

type consoleBootstrapScope struct {
	Environment string `json:"environment"`
	Namespace   string `json:"namespace,omitempty"`
}

type consoleBootstrapWorkspace struct {
	ID           string   `json:"id"`
	DefaultPath  string   `json:"default_path"`
	Capabilities []string `json:"capabilities"`
}

type consoleBootstrapResponse struct {
	SchemaVersion string                      `json:"schema_version"`
	Identity      consoleBootstrapIdentity    `json:"identity"`
	Scope         consoleBootstrapScope       `json:"scope"`
	Workspaces    []consoleBootstrapWorkspace `json:"workspaces"`
	Features      []string                    `json:"features"`
}

// ConsoleBootstrap exposes only the caller-safe information needed to choose
// the console workspace and navigation. It is advisory UI metadata; GraphJin's
// normal root and action authorization remains authoritative for every request.
func (s *HttpService) ConsoleBootstrap(ah auth.HandlerFunc) http.Handler {
	h := s.apiV1ConsoleBootstrap(nil)
	return apiV1Handler(s, nil, h, ah)
}

// ConsoleBootstrapWithNS is the namespaced console bootstrap endpoint.
func (s *HttpService) ConsoleBootstrapWithNS(ah auth.HandlerFunc, ns string) http.Handler {
	h := s.apiV1ConsoleBootstrap(&ns)
	return apiV1Handler(s, &ns, h, ah)
}

func (s1 *HttpService) apiV1ConsoleBootstrap(ns *string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		s := s1.Load().(*graphjinService)
		ctx := s.applyIdentityContext(r.Context())
		role := runtimeRoleClass(ctx)
		roots := consoleReadableRoots(s.conf, role)
		features := consoleEnabledFeatures(s.conf)
		workspaces := []consoleBootstrapWorkspace{{
			ID:           "user",
			DefaultPath:  "/user/agent",
			Capabilities: consoleWorkspaceCapabilities(roots, features, "user"),
		}}
		if consoleAdminWorkspaceAvailable(roots, features) {
			workspaces = append(workspaces, consoleBootstrapWorkspace{
				ID:           "admin",
				DefaultPath:  "/admin/runtime",
				Capabilities: consoleWorkspaceCapabilities(roots, features, "admin"),
			})
		}
		if consoleAdminWorkspaceAvailable(roots, features) && evalReportsDirectoryAvailable(s.conf) {
			workspaces = append(workspaces, consoleBootstrapWorkspace{
				ID:           "trainer",
				DefaultPath:  "/trainer/reports",
				Capabilities: []string{evalReportsCapability},
			})
		}

		identity := consoleBootstrapIdentity{
			Role:          role,
			Authenticated: ctx.Value(core.UserIDKey) != nil,
		}
		if userID := ctx.Value(core.UserIDKey); userID != nil {
			identity.DisplayName = fmt.Sprint(userID)
		} else {
			identity.Suggested = s.consoleSuggestedIdentity()
		}
		scope := consoleBootstrapScope{Environment: effectiveMode(s.conf)}
		if ns != nil {
			scope.Namespace = *ns
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "private, no-store")
		_ = json.NewEncoder(w).Encode(consoleBootstrapResponse{
			SchemaVersion: consoleBootstrapSchemaVersion,
			Identity:      identity,
			Scope:         scope,
			Workspaces:    workspaces,
			Features:      features,
		})
	})
}

// consoleSuggestedIdentity offers the operator identity this service seeds work
// under, so a zero-configuration console starts inside the owner scope that
// already has content instead of an empty one.
//
// It is offered only where the console could actually use it: header-trusting
// development auth. Under JWT or in production the headers would be ignored, so
// suggesting an identity there would be advertising something that cannot work.
func (s *graphjinService) consoleSuggestedIdentity() *consoleSuggestedIdentity {
	seed := s.operatorSeed
	if seed == nil || strings.TrimSpace(seed.UserID) == "" {
		return nil
	}
	if s.conf == nil || s.conf.Serv.Production || !s.conf.Auth.Development {
		return nil
	}
	return &consoleSuggestedIdentity{
		UserID:    seed.UserID,
		Role:      seed.consoleSeedRole(),
		AccountID: seed.AccountID,
		Reason:    "development operator this service seeds standing questions under",
	}
}

func consoleReadableRoots(conf *Config, role string) []string {
	roots := registeredSystemRoots(conf)
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		if systemRootAllowed(conf, root, systemActionRead, role) {
			out = append(out, root)
		}
	}
	sort.Strings(out)
	return out
}

func consoleEnabledFeatures(conf *Config) []string {
	if conf == nil {
		return nil
	}
	features := make([]string, 0)
	for _, definition := range featurecap.SortedDefinitions() {
		if conf.effectiveFeatureCapability(definition.Kind, definition.Key) {
			features = append(features, definition.Kind+":"+definition.Key)
		}
	}
	return features
}

func consoleAdminWorkspaceAvailable(roots, features []string) bool {
	for _, root := range roots {
		switch root {
		case "gj_runtime", "gj_security", "gj_config":
			return true
		}
	}
	for _, feature := range features {
		if feature == featurecap.KindSystem+":"+featurecap.KeyDevToolsRead ||
			feature == featurecap.KindSystem+":"+featurecap.KeyRawGraphQLQuery {
			return true
		}
	}
	return false
}

func consoleWorkspaceCapabilities(roots, features []string, workspace string) []string {
	allowedRoots := map[string]bool{}
	switch strings.ToLower(workspace) {
	case "user":
		for _, root := range []string{"gj_artifacts", "gj_watch", "gj_watch_event", "gj_task", "gj_task_entry"} {
			allowedRoots[root] = true
		}
	default:
		for _, root := range []string{"gj_catalog", "gj_runtime", "gj_security", "gj_config", "gj_workflow", "gj_workflow_execution"} {
			allowedRoots[root] = true
		}
	}
	out := make([]string, 0, len(roots)+len(features)+1)
	if workspace == "user" {
		out = append(out, "agent.status")
	}
	for _, root := range roots {
		if allowedRoots[root] {
			out = append(out, root)
		}
	}
	if workspace == "admin" {
		out = append(out, features...)
	}
	sort.Strings(out)
	return out
}
