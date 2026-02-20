package serv

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/mark3labs/mcp-go/mcp"
)

const onboardingCandidateTTL = 15 * time.Minute

type cachedDiscoveredCandidate struct {
	Candidate DiscoveredDatabase
	CreatedAt time.Time
}

func (ms *mcpServer) registerOnboardingTools() {
	if !ms.service.conf.MCP.AllowDevTools {
		return
	}

	ms.srv.AddTool(mcp.NewTool(
		"plan_database_setup",
		mcp.WithDescription("Plan database setup without changing config. Returns ranked candidates and required next actions. "+
			"Response includes machine-readable next-step guidance in the `next` field."),
		mcp.WithArray("targets",
			mcp.Description("Optional explicit targets to check. Each item: {type?, host, port?, user?, password?, dbname?}."),
			mcp.Items(map[string]any{
				"type":     "object",
				"required": []string{"host"},
				"properties": map[string]any{
					"type":     map[string]any{"type": "string", "description": "Database type (postgres, mysql, mssql, oracle, mongodb)"},
					"host":     map[string]any{"type": "string", "description": "Hostname or IP address"},
					"port":     map[string]any{"type": "number", "description": "Port number"},
					"user":     map[string]any{"type": "string", "description": "Username for authentication"},
					"password": map[string]any{"type": "string", "description": "Password for authentication"},
					"dbname":   map[string]any{"type": "string", "description": "Database name"},
				},
			}),
		),
		mcp.WithBoolean("scan_local",
			mcp.Description("Scan localhost ports and sqlite files (default true)."),
		),
		mcp.WithBoolean("scan_unix_sockets",
			mcp.Description("Scan local Unix sockets for PostgreSQL/MySQL/MongoDB (default false)."),
		),
	), ms.handlePlanDatabaseSetup)

	ms.srv.AddTool(mcp.NewTool(
		"test_database_connection",
		mcp.WithDescription("Test one candidate or explicit database config without mutating GraphJin config. "+
			"Response includes machine-readable next-step guidance in the `next` field."),
		mcp.WithString("candidate_id",
			mcp.Description("Candidate ID from discover_databases/plan_database_setup."),
		),
		mcp.WithObject("config",
			mcp.Description("Explicit config: {type, host?, port?, path?, user?, password?, dbname?}."),
		),
		mcp.WithObject("discovery_options",
			mcp.Description("Optional options used when resolving candidate_id."),
		),
		mcp.WithObject("candidate_snapshot",
			mcp.Description("Optional candidate object from a previous discover/plan call."),
		),
	), ms.handleTestDatabaseConnection)

	ms.srv.AddTool(mcp.NewTool(
		"get_onboarding_status",
		mcp.WithDescription("Get current onboarding status: configured databases, active database, schema readiness and table counts. "+
			"Response includes machine-readable next-step guidance in the `next` field."),
	), ms.handleGetOnboardingStatus)

	if ms.service.conf.MCP.AllowConfigUpdates {
		ms.srv.AddTool(mcp.NewTool(
			"apply_database_setup",
			mcp.WithDescription("Apply selected database setup to GraphJin config and reload schema. Requires explicit selection. "+
				"Response includes machine-readable next-step guidance in the `next` field."),
			mcp.WithString("candidate_id",
				mcp.Description("Candidate ID to apply (recommended)."),
			),
			mcp.WithObject("config",
				mcp.Description("Explicit config override: {type, host?, port?, path?, user?, password?, dbname?}."),
			),
			mcp.WithString("database_alias",
				mcp.Description("Config key for this database. Defaults to dbname or graphjin_dev."),
			),
			mcp.WithBoolean("create_if_not_exists",
				mcp.Description("Create database before connect (dev mode only)."),
			),
			mcp.WithObject("discovery_options",
				mcp.Description("Optional options used when resolving candidate_id."),
			),
			mcp.WithObject("candidate_snapshot",
				mcp.Description("Optional candidate object from a previous discover/plan call."),
			),
			mcp.WithBoolean("allow_unverified_apply",
				mcp.Description("When false (default), setup is blocked unless candidate auth_status is 'ok'."),
			),
		), ms.handleApplyDatabaseSetup)
	}
}

type SetupPlanResult struct {
	Candidates  []DiscoveredDatabase `json:"candidates"`
	Checklist   []string             `json:"selection_checklist"`
	Recommended string               `json:"recommended_candidate_id,omitempty"`
	Next        *NextGuidance        `json:"next,omitempty"`
}

func (ms *mcpServer) handlePlanDatabaseSetup(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	result, err := ms.runDiscovery(args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	ms.cacheCandidates(result.Databases)

	plan := SetupPlanResult{
		Candidates: result.Databases,
		Checklist: []string{
			"Pick candidate_id explicitly (no automatic apply).",
			"Ensure auth_status is 'ok' or run test_database_connection with credentials.",
			"Choose a non-system dbname when possible.",
			"Call apply_database_setup with candidate_id + database_alias.",
		},
	}
	if len(result.Databases) > 0 {
		plan.Recommended = result.Databases[0].CandidateID
	}
	plan.Next = ms.nextForSetupPlan(plan)

	data, err := mcpMarshalJSON(plan, true)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

type ConnectionTestResult struct {
	Success       bool               `json:"success"`
	Candidate     DiscoveredDatabase `json:"candidate"`
	RecommendedDB string             `json:"recommended_dbname,omitempty"`
	Next          *NextGuidance      `json:"next,omitempty"`
}

func (ms *mcpServer) handleTestDatabaseConnection(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	candidate, err := ms.resolveCandidate(args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	user, _ := candidate.ConfigSnippet["user"].(string)
	password, _ := candidate.ConfigSnippet["password"].(string)
	probeDatabase(&candidate, user, password)
	enrichDiscoveredDatabase(&candidate)
	ms.cacheCandidates([]DiscoveredDatabase{candidate})

	test := ConnectionTestResult{
		Success:   candidate.AuthStatus == "ok",
		Candidate: candidate,
	}
	if dbname, _ := candidate.ConfigSnippet["dbname"].(string); dbname != "" {
		test.RecommendedDB = dbname
	}
	test.Next = ms.nextForConnectionTest(test)

	data, err := mcpMarshalJSON(test, true)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

type OnboardingStatusResult struct {
	ConfiguredDatabases []string      `json:"configured_databases"`
	ActiveDatabase      string        `json:"active_database,omitempty"`
	SchemaReady         bool          `json:"schema_ready"`
	TableCount          int           `json:"table_count"`
	Warnings            []string      `json:"warnings,omitempty"`
	Next                *NextGuidance `json:"next,omitempty"`
}

func (ms *mcpServer) handleGetOnboardingStatus(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	conf := &ms.service.conf.Core
	result := OnboardingStatusResult{
		ConfiguredDatabases: make([]string, 0, len(conf.Databases)),
		ActiveDatabase:      ms.getActiveDatabase(),
	}
	for name := range conf.Databases {
		result.ConfiguredDatabases = append(result.ConfiguredDatabases, name)
	}
	if ms.service.gj != nil {
		result.SchemaReady = ms.service.gj.SchemaReady()
		if result.SchemaReady {
			result.TableCount = len(ms.service.gj.GetTables())
		}
	}
	if len(result.ConfiguredDatabases) == 0 {
		result.Warnings = append(result.Warnings, "no configured databases")
	}
	if !result.SchemaReady {
		result.Warnings = append(result.Warnings, "schema not ready")
	}
	result.Next = ms.nextForOnboardingStatus(result)

	data, err := mcpMarshalJSON(result, true)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

type ApplyDatabaseSetupResult struct {
	Applied       bool              `json:"applied"`
	Success       bool              `json:"success"`
	Message       string            `json:"message"`
	DatabaseAlias string            `json:"database_alias,omitempty"`
	TableCount    int               `json:"table_count"`
	Tables        []string          `json:"tables,omitempty"`
	Verification  ApplyVerification `json:"verification"`
	Errors        []string          `json:"errors,omitempty"`
	Next          *NextGuidance     `json:"next,omitempty"`
}

type ApplyVerification struct {
	AuthStatus  string `json:"auth_status,omitempty"`
	ProbeStatus string `json:"probe_status_code,omitempty"`
	AuthError   string `json:"auth_error,omitempty"`
}

func (ms *mcpServer) handleApplyDatabaseSetup(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	candidate, err := ms.resolveCandidate(args)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	allowUnverifiedApply := false
	if v, ok := args["allow_unverified_apply"].(bool); ok {
		allowUnverifiedApply = v
	}

	createIfNotExists := false
	if v, ok := args["create_if_not_exists"].(bool); ok {
		createIfNotExists = v && !ms.service.conf.Serv.Production
	}
	dbAlias, _ := args["database_alias"].(string)
	if dbAlias == "" {
		if n, _ := candidate.ConfigSnippet["dbname"].(string); n != "" {
			dbAlias = n
		} else {
			dbAlias = "graphjin_dev"
		}
	}

	dbType := strings.ToLower(candidate.Type)
	host := candidate.Host
	port := candidate.Port
	dbName, _ := candidate.ConfigSnippet["dbname"].(string)
	user, _ := candidate.ConfigSnippet["user"].(string)
	password, _ := candidate.ConfigSnippet["password"].(string)
	path, _ := candidate.ConfigSnippet["path"].(string)

	probeDatabase(&candidate, user, password)
	enrichDiscoveredDatabase(&candidate)
	ms.cacheCandidates([]DiscoveredDatabase{candidate})
	verification := ApplyVerification{
		AuthStatus:  candidate.AuthStatus,
		ProbeStatus: candidate.ProbeStatus,
		AuthError:   candidate.AuthError,
	}

	if !allowUnverifiedApply && candidate.AuthStatus != "ok" {
		out := ApplyDatabaseSetupResult{
			Applied:      false,
			Success:      false,
			Message:      "candidate verification failed; config not applied",
			Verification: verification,
			Errors: []string{
				"candidate is not verified (auth_status must be 'ok')",
				"use test_database_connection with credentials, or set allow_unverified_apply: true",
			},
		}
		out.Next = ms.nextForApplyDatabaseSetup(out)
		data, mErr := mcpMarshalJSON(out, true)
		if mErr != nil {
			return mcp.NewToolResultError(mErr.Error()), nil
		}
		return mcp.NewToolResultText(string(data)), nil
	}

	if dbName == "" {
		dbName = dbAlias
	}

	conf := &ms.service.conf.Core
	if conf.Databases == nil {
		conf.Databases = make(map[string]core.DatabaseConfig)
	}
	conf.Databases[dbAlias] = core.DatabaseConfig{
		Type:     dbType,
		Host:     host,
		Port:     port,
		DBName:   dbName,
		User:     user,
		Password: password,
		Path:     path,
	}

	if createIfNotExists {
		if err := createDatabaseOnServer(dbType, host, port, user, password, dbName, ms.service.log); err != nil {
			ms.service.log.Warnf("apply_database_setup create: %v", err)
		}
	}

	var errs []string
	var tableNames []string
	if ms.service.gj != nil {
		syncDBFromDatabases(ms.service.conf)
		ms.ensureDBConnections()
		if err := ms.service.gj.Reload(); err != nil {
			errs = append(errs, fmt.Sprintf("reload failed: %v", err))
		} else if ms.service.gj.SchemaReady() {
			for _, t := range ms.service.gj.GetTables() {
				tableNames = append(tableNames, t.Name)
			}
		}
	} else {
		if _, err := ms.tryInitializeGraphJin(createIfNotExists); err != nil {
			errs = append(errs, fmt.Sprintf("initialization failed: %v", err))
		} else if ms.service.gj != nil && ms.service.gj.SchemaReady() {
			for _, t := range ms.service.gj.GetTables() {
				tableNames = append(tableNames, t.Name)
			}
		}
	}

	if !ms.service.conf.Serv.Production {
		if err := ms.saveConfigToDisk(); err != nil {
			ms.service.log.Warnf("apply_database_setup save: %v", err)
		}
	}

	out := ApplyDatabaseSetupResult{
		Applied:       true,
		Success:       len(errs) == 0,
		Message:       "database setup applied",
		DatabaseAlias: dbAlias,
		TableCount:    len(tableNames),
		Tables:        tableNames,
		Verification:  verification,
		Errors:        errs,
	}
	if len(errs) > 0 {
		out.Message = "database setup applied with errors"
	}
	out.Next = ms.nextForApplyDatabaseSetup(out)

	data, err := mcpMarshalJSON(out, true)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

func (ms *mcpServer) resolveCandidate(args map[string]any) (DiscoveredDatabase, error) {
	if cfgAny, ok := args["config"].(map[string]any); ok && len(cfgAny) > 0 {
		db, err := parseConfigAsCandidate(cfgAny)
		if err != nil {
			return DiscoveredDatabase{}, err
		}
		return db, nil
	}

	if snapshotAny, ok := args["candidate_snapshot"].(map[string]any); ok && len(snapshotAny) > 0 {
		db, err := parseSnapshotCandidate(snapshotAny)
		if err != nil {
			return DiscoveredDatabase{}, err
		}
		return db, nil
	}

	candidateID, _ := args["candidate_id"].(string)
	if candidateID == "" {
		return DiscoveredDatabase{}, fmt.Errorf("provide candidate_id, candidate_snapshot, or config")
	}
	if c, ok := ms.getCachedCandidate(candidateID); ok {
		return c, nil
	}
	if opts, ok := args["discovery_options"].(map[string]any); ok && len(opts) > 0 {
		result, err := ms.runDiscovery(opts)
		if err != nil {
			return DiscoveredDatabase{}, err
		}
		ms.cacheCandidates(result.Databases)
		for _, c := range result.Databases {
			if c.CandidateID == candidateID {
				return c, nil
			}
		}
	}

	return DiscoveredDatabase{}, fmt.Errorf(
		"candidate_id not found: %s (provide discovery_options to rerun discovery, or pass candidate_snapshot/config)",
		candidateID)
}

func parseConfigAsCandidate(cfgAny map[string]any) (DiscoveredDatabase, error) {
	db := DiscoveredDatabase{
		Status:        "explicit",
		Source:        "manual",
		ConfigSnippet: map[string]any{},
	}
	db.Type, _ = cfgAny["type"].(string)
	db.Host, _ = cfgAny["host"].(string)
	if p, ok := cfgAny["port"].(float64); ok {
		db.Port = int(p)
	}
	db.FilePath, _ = cfgAny["path"].(string)
	if u, ok := cfgAny["user"].(string); ok {
		db.ConfigSnippet["user"] = u
	}
	if p, ok := cfgAny["password"].(string); ok {
		db.ConfigSnippet["password"] = p
	}
	if n, ok := cfgAny["dbname"].(string); ok {
		db.ConfigSnippet["dbname"] = n
	}
	if db.Type == "" {
		return DiscoveredDatabase{}, fmt.Errorf("config.type is required")
	}
	if db.Type == "sqlite" {
		if db.FilePath == "" {
			return DiscoveredDatabase{}, fmt.Errorf("config.path is required for sqlite")
		}
		db.ConfigSnippet["path"] = db.FilePath
	}
	if db.ConfigSnippet["type"] == nil {
		db.ConfigSnippet["type"] = db.Type
	}
	db.CandidateID = buildCandidateID(db)
	return db, nil
}

func parseSnapshotCandidate(snapshot map[string]any) (DiscoveredDatabase, error) {
	cfg := map[string]any{}
	if v, ok := snapshot["type"].(string); ok {
		cfg["type"] = v
	}
	if v, ok := snapshot["host"].(string); ok {
		cfg["host"] = v
	}
	if v, ok := snapshot["port"].(float64); ok {
		cfg["port"] = v
	}
	if v, ok := snapshot["file_path"].(string); ok {
		cfg["path"] = v
	}
	if cs, ok := snapshot["config_snippet"].(map[string]any); ok {
		for _, key := range []string{"user", "password", "dbname", "path", "type", "host", "port"} {
			if _, exists := cfg[key]; !exists {
				if cv, ok := cs[key]; ok {
					cfg[key] = cv
				}
			}
		}
	}
	db, err := parseConfigAsCandidate(cfg)
	if err != nil {
		return DiscoveredDatabase{}, err
	}
	if v, ok := snapshot["candidate_id"].(string); ok && v != "" {
		db.CandidateID = v
	}
	return db, nil
}

func (ms *mcpServer) cacheCandidates(candidates []DiscoveredDatabase) {
	if len(candidates) == 0 || ms.service == nil {
		return
	}
	now := time.Now()
	ms.service.onboardingMu.Lock()
	defer ms.service.onboardingMu.Unlock()
	if ms.service.onboardingCandidates == nil {
		ms.service.onboardingCandidates = make(map[string]cachedDiscoveredCandidate)
	}
	// Lazy cleanup
	for k, v := range ms.service.onboardingCandidates {
		if now.Sub(v.CreatedAt) > onboardingCandidateTTL {
			delete(ms.service.onboardingCandidates, k)
		}
	}
	for _, c := range candidates {
		if c.CandidateID == "" {
			c.CandidateID = buildCandidateID(c)
		}
		ms.service.onboardingCandidates[c.CandidateID] = cachedDiscoveredCandidate{
			Candidate: c,
			CreatedAt: now,
		}
	}
}

func (ms *mcpServer) getCachedCandidate(candidateID string) (DiscoveredDatabase, bool) {
	if ms.service == nil || candidateID == "" {
		return DiscoveredDatabase{}, false
	}
	now := time.Now()
	ms.service.onboardingMu.Lock()
	defer ms.service.onboardingMu.Unlock()
	if ms.service.onboardingCandidates == nil {
		return DiscoveredDatabase{}, false
	}
	entry, ok := ms.service.onboardingCandidates[candidateID]
	if !ok {
		return DiscoveredDatabase{}, false
	}
	if now.Sub(entry.CreatedAt) > onboardingCandidateTTL {
		delete(ms.service.onboardingCandidates, candidateID)
		return DiscoveredDatabase{}, false
	}
	return entry.Candidate, true
}
