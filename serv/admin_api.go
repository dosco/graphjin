package serv

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// ConfigSection represents a group of related config fields for the Web UI
type ConfigSection struct {
	Name        string        `json:"name"`
	Title       string        `json:"title"`
	Description string        `json:"description,omitempty"`
	Fields      []ConfigField `json:"fields"`
}

// ConfigField represents a single config field with metadata for dynamic rendering
type ConfigField struct {
	Key         string      `json:"key"`
	Label       string      `json:"label"`
	Value       interface{} `json:"value"`
	Type        string      `json:"type"` // string, bool, int, duration, array, object, role, table
	Sensitive   bool        `json:"sensitive,omitempty"`
	Description string      `json:"description,omitempty"`
}

// Helper functions to create config fields

func field(key, label string, value interface{}, fieldType string) ConfigField {
	return ConfigField{Key: key, Label: label, Value: value, Type: fieldType}
}

func sensitiveField(key, label string) ConfigField {
	return ConfigField{Key: key, Label: label, Value: "****", Type: "string", Sensitive: true}
}

func boolField(key, label string, value bool) ConfigField {
	return ConfigField{Key: key, Label: label, Value: value, Type: "bool"}
}

func intField(key, label string, value interface{}) ConfigField {
	return ConfigField{Key: key, Label: label, Value: value, Type: "int"}
}

func durationField(key, label string, value string) ConfigField {
	if value == "" || value == "0s" {
		value = "0s"
	}
	return ConfigField{Key: key, Label: label, Value: value, Type: "duration"}
}

func arrayField(key, label string, value []string) ConfigField {
	return ConfigField{Key: key, Label: label, Value: value, Type: "array"}
}

// Admin API REST endpoints for the Web UI
// These endpoints expose schema, queries, config, and database info
// These routes are only registered when WebUI is enabled (dev mode only)

// writeJSON encodes data as JSON and writes to response, handling errors
func writeJSON(w http.ResponseWriter, data interface{}) {
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, "encoding error", http.StatusInternalServerError)
	}
}

// writeJSONError writes a JSON error response with proper header ordering
func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	writeJSON(w, map[string]string{"error": message})
}

// adminTablesHandler returns list of all database tables
// GET /api/v1/admin/tables
func adminTablesHandler(s1 *HttpService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		s := s1.Load().(*graphjinService)
		w.Header().Set("Content-Type", "application/json")

		tables := s.gj.GetTables()

		writeJSON(w, map[string]interface{}{
			"tables": tables,
			"count":  len(tables),
		})
	})
}

// adminTableSchemaHandler returns detailed schema for a specific table
// GET /api/v1/admin/tables/{name}
func adminTableSchemaHandler(s1 *HttpService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		s := s1.Load().(*graphjinService)

		// Extract table name from URL path: /api/v1/admin/tables/{name}
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/tables/")
		tableName := strings.TrimSuffix(path, "/")

		// URL decode the table name
		var err error
		tableName, err = url.PathUnescape(tableName)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid table name encoding")
			return
		}

		if tableName == "" {
			writeJSONError(w, http.StatusBadRequest, "table name required")
			return
		}

		schema, err := s.gj.GetTableSchema(tableName)
		if err != nil {
			writeJSONError(w, http.StatusNotFound, err.Error())
			return
		}

		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, schema)
	})
}

// adminQueriesHandler returns list of all saved queries
// GET /api/v1/admin/queries
func adminQueriesHandler(s1 *HttpService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		s := s1.Load().(*graphjinService)

		w.Header().Set("Content-Type", "application/json")

		queries, err := s.gj.ListSavedQueries()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, map[string]interface{}{
			"queries": queries,
			"count":   len(queries),
		})
	})
}

// adminQueryDetailHandler returns details of a specific saved query
// GET /api/v1/admin/queries/{name}
func adminQueryDetailHandler(s1 *HttpService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		s := s1.Load().(*graphjinService)

		// Extract query name from URL path: /api/v1/admin/queries/{name}
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/queries/")
		queryName := strings.TrimSuffix(path, "/")

		// URL decode the query name
		var err error
		queryName, err = url.PathUnescape(queryName)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid query name encoding")
			return
		}

		if queryName == "" {
			writeJSONError(w, http.StatusBadRequest, "query name required")
			return
		}

		query, err := s.gj.GetSavedQuery(queryName)
		if err != nil {
			writeJSONError(w, http.StatusNotFound, err.Error())
			return
		}

		w.Header().Set("Content-Type", "application/json")
		writeJSON(w, query)
	})
}

// adminFragmentsHandler returns list of all fragments
// GET /api/v1/admin/fragments
func adminFragmentsHandler(s1 *HttpService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		s := s1.Load().(*graphjinService)

		w.Header().Set("Content-Type", "application/json")

		fragments, err := s.gj.ListFragments()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error())
			return
		}

		writeJSON(w, map[string]interface{}{
			"fragments": fragments,
			"count":     len(fragments),
		})
	})
}

// adminConfigHandler returns sanitized configuration with schema-driven structure
// GET /api/v1/admin/config
func adminConfigHandler(s1 *HttpService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		s := s1.Load().(*graphjinService)

		w.Header().Set("Content-Type", "application/json")

		// Build config sections dynamically from s.conf
		sections := []ConfigSection{
			buildGeneralSection(s.conf),
			buildServerSection(s.conf),
			buildCORSSection(s.conf),
			buildDatabaseSection(s.conf),
			buildAuthSection(s.conf),
			buildMCPSection(s.conf),
			buildRateLimiterSection(s.conf),
			buildCompilerSection(s.conf),
			buildSecuritySection(s.conf),
			buildRolesSection(s.conf),
			buildTablesSection(s.conf),
			buildFunctionsSection(s.conf),
			buildResolversSection(s.conf),
		}

		writeJSON(w, map[string]interface{}{
			"sections": sections,
		})
	})
}

// Section builder functions - each builds a ConfigSection with relevant fields

func buildGeneralSection(conf *Config) ConfigSection {
	return ConfigSection{
		Name:  "general",
		Title: "General Settings",
		Fields: []ConfigField{
			field("appName", "App Name", conf.AppName, "string"),
			boolField("production", "Production Mode", conf.Serv.Production),
			field("logLevel", "Log Level", conf.LogLevel, "string"),
			field("logFormat", "Log Format", conf.LogFormat, "string"),
			boolField("debug", "Debug", conf.Debug),
		},
	}
}

func buildServerSection(conf *Config) ConfigSection {
	hostPort := conf.HostPort
	if hostPort == "" && (conf.Host != "" || conf.Port != "") {
		hostPort = conf.Host + ":" + conf.Port
	}

	return ConfigSection{
		Name:  "server",
		Title: "Server",
		Fields: []ConfigField{
			field("hostPort", "Host:Port", hostPort, "string"),
			field("host", "Host", conf.Host, "string"),
			field("port", "Port", conf.Port, "string"),
			boolField("httpGZip", "HTTP Compression", conf.HTTPGZip),
			boolField("serverTiming", "Server Timing Header", conf.ServerTiming),
			field("cacheControl", "Cache-Control Header", conf.CacheControl, "string"),
			boolField("webUI", "Web UI", conf.WebUI),
			boolField("enableTracing", "Tracing", conf.EnableTracing),
			boolField("watchAndReload", "Watch & Reload", conf.WatchAndReload),
		},
	}
}

func buildCORSSection(conf *Config) ConfigSection {
	return ConfigSection{
		Name:  "cors",
		Title: "CORS",
		Fields: []ConfigField{
			arrayField("allowedOrigins", "Allowed Origins", conf.AllowedOrigins),
			arrayField("allowedHeaders", "Allowed Headers", conf.AllowedHeaders),
			boolField("debugCORS", "Debug CORS", conf.DebugCORS),
		},
	}
}

func buildDatabaseSection(conf *Config) ConfigSection {
	db := conf.DB
	fields := []ConfigField{
		field("type", "Type", db.Type, "string"),
		field("host", "Host", db.Host, "string"),
		intField("port", "Port", db.Port),
		field("dbName", "Database Name", db.DBName, "string"),
		field("schema", "Schema", db.Schema, "string"),
		intField("poolSize", "Pool Size", db.PoolSize),
		intField("maxConnections", "Max Connections", db.MaxConnections),
		durationField("maxConnIdleTime", "Max Conn Idle Time", db.MaxConnIdleTime.String()),
		durationField("maxConnLifeTime", "Max Conn Lifetime", db.MaxConnLifeTime.String()),
		durationField("pingTimeout", "Ping Timeout", db.PingTimeout.String()),
		boolField("enableTLS", "TLS Enabled", db.EnableTLS),
	}

	// Add sensitive fields (masked)
	if db.Password != "" {
		fields = append(fields, sensitiveField("password", "Password"))
	}
	if db.ConnString != "" {
		fields = append(fields, sensitiveField("connString", "Connection String"))
	}
	if db.ServerName != "" {
		fields = append(fields, sensitiveField("serverName", "TLS Server Name"))
	}
	if db.ServerCert != "" {
		fields = append(fields, sensitiveField("serverCert", "Server Certificate"))
	}
	if db.ClientCert != "" {
		fields = append(fields, sensitiveField("clientCert", "Client Certificate"))
	}
	if db.ClientKey != "" {
		fields = append(fields, sensitiveField("clientKey", "Client Key"))
	}

	return ConfigSection{
		Name:   "database",
		Title:  "Database",
		Fields: fields,
	}
}

func buildAuthSection(conf *Config) ConfigSection {
	auth := conf.Auth
	fields := []ConfigField{
		boolField("development", "Development Mode", auth.Development),
		field("name", "Name", auth.Name, "string"),
		field("type", "Type", auth.Type, "string"),
		field("cookie", "Cookie Name", auth.Cookie, "string"),
	}

	// JWT settings (non-sensitive)
	if auth.JWT.Provider != "" {
		fields = append(fields, field("jwtProvider", "JWT Provider", auth.JWT.Provider, "string"))
	}
	if auth.JWT.Audience != "" {
		fields = append(fields, field("jwtAudience", "JWT Audience", auth.JWT.Audience, "string"))
	}
	if auth.JWT.Issuer != "" {
		fields = append(fields, field("jwtIssuer", "JWT Issuer", auth.JWT.Issuer, "string"))
	}
	if auth.JWT.JWKSURL != "" {
		fields = append(fields, field("jwksURL", "JWKS URL", auth.JWT.JWKSURL, "string"))
	}
	if auth.JWT.PubKeyType != "" {
		fields = append(fields, field("pubKeyType", "Public Key Type", auth.JWT.PubKeyType, "string"))
	}

	// Sensitive JWT fields (masked)
	if auth.JWT.Secret != "" {
		fields = append(fields, sensitiveField("jwtSecret", "JWT Secret"))
	}
	if auth.JWT.PubKey != "" {
		fields = append(fields, sensitiveField("pubKey", "Public Key"))
	}

	// Header auth settings
	if auth.Header.Name != "" {
		fields = append(fields, field("headerName", "Header Name", auth.Header.Name, "string"))
		fields = append(fields, boolField("headerExists", "Header Exists Check", auth.Header.Exists))
	}

	return ConfigSection{
		Name:   "auth",
		Title:  "Authentication",
		Fields: fields,
	}
}

func buildMCPSection(conf *Config) ConfigSection {
	mcp := conf.MCP
	return ConfigSection{
		Name:  "mcp",
		Title: "MCP (Model Context Protocol)",
		Fields: []ConfigField{
			boolField("disabled", "Disabled", mcp.Disable),
			boolField("enableSearch", "Enable Search", mcp.EnableSearch),
			boolField("allowMutations", "Allow Mutations", mcp.AllowMutations),
			boolField("allowRawQueries", "Allow Raw Queries", mcp.AllowRawQueries),
			field("stdioUserID", "Stdio User ID", mcp.StdioUserID, "string"),
			field("stdioUserRole", "Stdio User Role", mcp.StdioUserRole, "string"),
		},
	}
}

func buildRateLimiterSection(conf *Config) ConfigSection {
	rl := conf.RateLimiter
	return ConfigSection{
		Name:  "rateLimiter",
		Title: "Rate Limiter",
		Fields: []ConfigField{
			boolField("enabled", "Enabled", rl.Rate > 0),
			field("rate", "Rate (events/sec)", rl.Rate, "float"),
			intField("bucket", "Bucket Size", rl.Bucket),
			field("ipHeader", "IP Header", rl.IPHeader, "string"),
		},
	}
}

func buildCompilerSection(conf *Config) ConfigSection {
	return ConfigSection{
		Name:  "compiler",
		Title: "Compiler Settings",
		Fields: []ConfigField{
			field("dbType", "Database Type", conf.DBType, "string"),
			boolField("defaultBlock", "Default Block", conf.DefaultBlock),
			intField("defaultLimit", "Default Limit", conf.DefaultLimit),
			boolField("disableAgg", "Disable Aggregations", conf.DisableAgg),
			boolField("disableFuncs", "Disable Functions", conf.DisableFuncs),
			boolField("enableCamelcase", "Enable Camelcase", conf.EnableCamelcase),
			boolField("enableSchema", "Enable Schema", conf.EnableSchema),
			boolField("enableIntrospection", "Enable Introspection", conf.EnableIntrospection),
			boolField("mockDB", "Mock Database", conf.MockDB),
			boolField("logVars", "Log Variables", conf.LogVars),
			durationField("dbSchemaPollDuration", "Schema Poll Duration", conf.DBSchemaPollDuration.String()),
			durationField("subsPollDuration", "Subscription Poll Duration", conf.SubsPollDuration.String()),
		},
	}
}

func buildSecuritySection(conf *Config) ConfigSection {
	fields := []ConfigField{
		boolField("disableAllowList", "Disable Allow List", conf.DisableAllowList),
		boolField("authFailBlock", "Auth Fail Block", conf.AuthFailBlock),
		boolField("setUserID", "Set User ID", conf.SetUserID),
		boolField("disableProdSecurity", "Disable Production Security", conf.DisableProdSecurity),
	}

	// Sensitive fields
	if conf.SecretKey != "" {
		fields = append(fields, sensitiveField("secretKey", "Secret Key"))
	}

	return ConfigSection{
		Name:   "security",
		Title:  "Security",
		Fields: fields,
	}
}

func buildRolesSection(conf *Config) ConfigSection {
	fields := make([]ConfigField, 0, len(conf.Roles))
	for _, role := range conf.Roles {
		// Build role object with tables summary
		tables := make([]map[string]interface{}, 0, len(role.Tables))
		for _, t := range role.Tables {
			tableInfo := map[string]interface{}{
				"name": t.Name,
			}
			if t.Schema != "" {
				tableInfo["schema"] = t.Schema
			}
			tableInfo["readOnly"] = t.ReadOnly

			// Permission summary
			perms := make([]string, 0, 4)
			if t.Query != nil && !t.Query.Block {
				perms = append(perms, "query")
			}
			if t.Insert != nil && !t.Insert.Block {
				perms = append(perms, "insert")
			}
			if t.Update != nil && !t.Update.Block {
				perms = append(perms, "update")
			}
			if t.Delete != nil && !t.Delete.Block {
				perms = append(perms, "delete")
			}
			tableInfo["permissions"] = perms
			tables = append(tables, tableInfo)
		}

		roleObj := map[string]interface{}{
			"name":        role.Name,
			"comment":     role.Comment,
			"match":       role.Match,
			"tableCount":  len(role.Tables),
			"tables":      tables,
		}
		fields = append(fields, ConfigField{
			Key:   role.Name,
			Label: role.Name,
			Value: roleObj,
			Type:  "role",
		})
	}
	return ConfigSection{
		Name:   "roles",
		Title:  "Roles",
		Fields: fields,
	}
}

func buildTablesSection(conf *Config) ConfigSection {
	fields := make([]ConfigField, 0, len(conf.Tables))
	for _, t := range conf.Tables {
		tableObj := map[string]interface{}{
			"name":        t.Name,
			"schema":      t.Schema,
			"type":        t.Type,
			"database":    t.Database,
			"blocklist":   t.Blocklist,
			"columnCount": len(t.Columns),
		}
		if t.Table != "" {
			tableObj["inherits"] = t.Table
		}
		if len(t.OrderBy) > 0 {
			tableObj["orderBy"] = t.OrderBy
		}
		fields = append(fields, ConfigField{
			Key:   t.Name,
			Label: t.Name,
			Value: tableObj,
			Type:  "table",
		})
	}
	return ConfigSection{
		Name:   "tables",
		Title:  "Table Configurations",
		Fields: fields,
	}
}

func buildFunctionsSection(conf *Config) ConfigSection {
	fields := make([]ConfigField, 0, len(conf.Functions))
	for _, f := range conf.Functions {
		funcObj := map[string]interface{}{
			"name":       f.Name,
			"schema":     f.Schema,
			"returnType": f.ReturnType,
		}
		fields = append(fields, ConfigField{
			Key:   f.Name,
			Label: f.Name,
			Value: funcObj,
			Type:  "function",
		})
	}
	return ConfigSection{
		Name:   "functions",
		Title:  "Functions",
		Fields: fields,
	}
}

func buildResolversSection(conf *Config) ConfigSection {
	fields := make([]ConfigField, 0, len(conf.Resolvers))
	for _, r := range conf.Resolvers {
		resolverObj := map[string]interface{}{
			"name":   r.Name,
			"type":   r.Type,
			"schema": r.Schema,
			"table":  r.Table,
			"column": r.Column,
		}
		if r.StripPath != "" {
			resolverObj["stripPath"] = r.StripPath
		}
		fields = append(fields, ConfigField{
			Key:   r.Name,
			Label: r.Name,
			Value: resolverObj,
			Type:  "resolver",
		})
	}
	return ConfigSection{
		Name:   "resolvers",
		Title:  "Resolvers",
		Fields: fields,
	}
}

// adminDatabaseHandler returns database info and connection pool stats
// GET /api/v1/admin/database
func adminDatabaseHandler(s1 *HttpService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		s := s1.Load().(*graphjinService)

		w.Header().Set("Content-Type", "application/json")

		// Get connection pool stats
		stats := s.db.Stats()

		// Get table and query counts
		tables := s.gj.GetTables()
		queries, _ := s.gj.ListSavedQueries()

		response := map[string]interface{}{
			"type":   s.conf.DB.Type,
			"host":   s.conf.DB.Host,
			"port":   s.conf.DB.Port,
			"dbName": s.conf.DB.DBName,
			"schema": s.conf.DB.Schema,
			"pool": map[string]interface{}{
				"maxOpen":           stats.MaxOpenConnections,
				"open":              stats.OpenConnections,
				"inUse":             stats.InUse,
				"idle":              stats.Idle,
				"waitCount":         stats.WaitCount,
				"waitDuration":      stats.WaitDuration.String(),
				"maxIdleClosed":     stats.MaxIdleClosed,
				"maxLifetimeClosed": stats.MaxLifetimeClosed,
			},
			"tableCount": len(tables),
			"queryCount": len(queries),
		}

		writeJSON(w, response)
	})
}

// adminDatabasesHandler returns info and stats for all configured databases
// GET /api/v1/admin/databases
func adminDatabasesHandler(s1 *HttpService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		s := s1.Load().(*graphjinService)

		w.Header().Set("Content-Type", "application/json")

		// Get stats for all databases
		databases := s.gj.GetAllDatabaseStats()

		writeJSON(w, map[string]interface{}{
			"databases": databases,
			"count":     len(databases),
		})
	})
}

