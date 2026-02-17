package serv

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/mark3labs/mcp-go/mcp"
)

// registerQuickSetupTools registers the quick_setup tool
func (ms *mcpServer) registerQuickSetupTools() {
	if !ms.service.conf.MCP.AllowDevTools || !ms.service.conf.MCP.AllowConfigUpdates {
		return
	}

	ms.srv.AddTool(mcp.NewTool(
		"quick_setup",
		mcp.WithDescription("Automatically discover databases, pick the best candidate, configure GraphJin, and verify the connection. "+
			"This combines discover_databases → select best → update_current_config → verify into a single step. "+
			"Use this for quick first-time setup. Optionally specify a preferred database type or name."),
		mcp.WithString("prefer_type",
			mcp.Description("Preferred database type (e.g., 'postgres', 'mysql'). If found, this type is prioritized."),
		),
		mcp.WithString("prefer_db",
			mcp.Description("Preferred database name. If found on any server, it's selected."),
		),
		mcp.WithBoolean("create_if_not_exists",
			mcp.Description("Create the database if it doesn't exist (dev mode only). Default: false"),
		),
	), ms.handleQuickSetup)
}

// QuickSetupResult represents the result of the quick_setup operation
type QuickSetupResult struct {
	Success       bool     `json:"success"`
	Message       string   `json:"message"`
	Steps         []string `json:"steps"`
	DatabaseType  string   `json:"database_type,omitempty"`
	DatabaseName  string   `json:"database_name,omitempty"`
	Host          string   `json:"host,omitempty"`
	Tables        []string `json:"tables,omitempty"`
	TableCount    int      `json:"table_count"`
	Errors        []string `json:"errors,omitempty"`
}

// handleQuickSetup runs the full discovery → configure → verify flow
func (ms *mcpServer) handleQuickSetup(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	preferType, _ := args["prefer_type"].(string)
	preferDB, _ := args["prefer_db"].(string)
	createIfNotExists := false
	if ci, ok := args["create_if_not_exists"].(bool); ok {
		createIfNotExists = ci
	}
	if createIfNotExists && ms.service.conf.Serv.Production {
		createIfNotExists = false
	}

	var steps []string
	var errors []string

	// Step 1: Discover databases
	steps = append(steps, "Scanning for databases...")
	discovered, _ := ms.discoverForQuickSetup()
	steps = append(steps, fmt.Sprintf("Found %d database server(s)", len(discovered)))

	if len(discovered) == 0 {
		result := QuickSetupResult{
			Success: false,
			Message: "No databases found. Start a database server and try again, or use update_current_config to manually configure a connection.",
			Steps:   steps,
		}
		data, _ := mcpMarshalJSON(result, true)
		return mcp.NewToolResultText(string(data)), nil
	}

	// Step 2: Pick the best candidate
	steps = append(steps, "Selecting best database...")
	best := ms.selectBestDatabase(discovered, preferType, preferDB)
	if best == nil {
		result := QuickSetupResult{
			Success: false,
			Message: "Found database servers but couldn't connect to any. Check credentials or use discover_databases for more details.",
			Steps:   steps,
			Errors:  errors,
		}
		data, _ := mcpMarshalJSON(result, true)
		return mcp.NewToolResultText(string(data)), nil
	}

	dbType := best.Type
	host := best.Host
	port := best.Port
	user, _ := best.ConfigSnippet["user"].(string)
	password, _ := best.ConfigSnippet["password"].(string)
	dbName, _ := best.ConfigSnippet["dbname"].(string)

	if dbName == "" && preferDB != "" {
		dbName = preferDB
	}
	if dbName == "" {
		dbName = "graphjin_dev"
	}

	steps = append(steps, fmt.Sprintf("Selected: %s on %s:%d, database: %s", dbType, host, port, dbName))

	// Step 3: Create database if requested
	if createIfNotExists {
		steps = append(steps, fmt.Sprintf("Creating database '%s' if not exists...", dbName))
		if err := createDatabaseOnServer(dbType, host, port, user, password, dbName, ms.service.log); err != nil {
			ms.service.log.Warnf("quick_setup create: %v", err)
			steps = append(steps, fmt.Sprintf("Create database warning: %v (may already exist)", err))
		} else {
			steps = append(steps, "Database created or already exists")
		}
	}

	// Step 4: Configure GraphJin
	steps = append(steps, "Configuring GraphJin...")
	conf := &ms.service.conf.Core
	if conf.Databases == nil {
		conf.Databases = make(map[string]core.DatabaseConfig)
	}
	conf.Databases[dbName] = core.DatabaseConfig{
		Type:     dbType,
		Host:     host,
		Port:     port,
		DBName:   dbName,
		User:     user,
		Password: password,
	}

	// Step 5: Initialize/reload
	steps = append(steps, "Connecting and loading schema...")
	var tableNames []string
	if ms.service.gj != nil {
		syncDBFromDatabases(ms.service.conf)
		if err := ms.service.gj.Reload(); err != nil {
			errors = append(errors, fmt.Sprintf("reload failed: %v", err))
		} else {
			if ms.service.gj.SchemaReady() {
				tables := ms.service.gj.GetTables()
				for _, t := range tables {
					tableNames = append(tableNames, t.Name)
				}
			}
		}
	} else {
		dbNames, err := ms.tryInitializeGraphJin(createIfNotExists)
		if err != nil {
			errors = append(errors, fmt.Sprintf("initialization failed: %v", err))
			if len(dbNames) > 0 {
				steps = append(steps, fmt.Sprintf("Available databases on server: %s", strings.Join(dbNames, ", ")))
			}
		} else {
			steps = append(steps, "GraphJin initialized successfully")
			if ms.service.gj != nil && ms.service.gj.SchemaReady() {
				tables := ms.service.gj.GetTables()
				for _, t := range tables {
					tableNames = append(tableNames, t.Name)
				}
			}
		}
	}

	if len(tableNames) > 0 {
		steps = append(steps, fmt.Sprintf("Discovered %d table(s)", len(tableNames)))
	}

	// Save config to disk in dev mode
	if !ms.service.conf.Serv.Production {
		if err := ms.saveConfigToDisk(); err != nil {
			ms.service.log.Warnf("quick_setup save: %v", err)
		} else {
			steps = append(steps, "Configuration saved to disk")
		}
	}

	result := QuickSetupResult{
		Success:      len(errors) == 0,
		Message:      "Quick setup completed",
		Steps:        steps,
		DatabaseType: dbType,
		DatabaseName: dbName,
		Host:         fmt.Sprintf("%s:%d", host, port),
		Tables:       tableNames,
		TableCount:   len(tableNames),
		Errors:       errors,
	}

	if len(errors) > 0 {
		result.Message = "Quick setup completed with errors"
	}

	data, err := mcpMarshalJSON(result, true)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// discoverForQuickSetup runs a lightweight discovery scan
func (ms *mcpServer) discoverForQuickSetup() ([]DiscoveredDatabase, error) {
	timeout := 500 * time.Millisecond

	tcpProbes := []dbProbe{
		{"postgres", 5432},
		{"mysql", 3306},
		{"mssql", 1433},
		{"mongodb", 27017},
	}

	var databases []DiscoveredDatabase
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, probe := range tcpProbes {
		wg.Add(1)
		go func(p dbProbe) {
			defer wg.Done()
			if checkTCPPort("127.0.0.1", p.port, timeout) {
				db := DiscoveredDatabase{
					Type:          p.dbType,
					Host:          "localhost",
					Port:          p.port,
					Source:        "tcp",
					Status:        "listening",
					ConfigSnippet: buildConfigSnippet(p.dbType, "localhost", p.port, ""),
				}
				// Probe with default credentials
				probeDatabase(&db, "", "")
				mu.Lock()
				databases = append(databases, db)
				mu.Unlock()
			}
		}(probe)
	}
	wg.Wait()

	return databases, nil
}

// selectBestDatabase picks the best database from discovered list
func (ms *mcpServer) selectBestDatabase(databases []DiscoveredDatabase, preferType, preferDB string) *DiscoveredDatabase {
	// Priority: matching preferred DB name > matching preferred type with auth > any with auth > any listening

	// First pass: find preferred database name match
	if preferDB != "" {
		for i := range databases {
			if databases[i].AuthStatus == "ok" {
				for _, name := range databases[i].Databases {
					if strings.EqualFold(name, preferDB) {
						databases[i].ConfigSnippet["dbname"] = name
						return &databases[i]
					}
				}
			}
		}
	}

	// Second pass: preferred type with auth ok
	if preferType != "" {
		for i := range databases {
			if strings.EqualFold(databases[i].Type, preferType) && databases[i].AuthStatus == "ok" {
				return &databases[i]
			}
		}
	}

	// Third pass: any with auth ok and user databases
	for i := range databases {
		if databases[i].AuthStatus == "ok" && len(databases[i].Databases) > 0 {
			filtered := filterSystemDatabases(databases[i].Type, databases[i].Databases)
			if len(filtered) > 0 {
				return &databases[i]
			}
		}
	}

	// Fourth pass: any with auth ok
	for i := range databases {
		if databases[i].AuthStatus == "ok" {
			return &databases[i]
		}
	}

	// Fifth pass: any listening
	for i := range databases {
		if databases[i].Status == "listening" {
			return &databases[i]
		}
	}

	return nil
}
