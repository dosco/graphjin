package serv

import (
	"reflect"
	"sort"
	"testing"

	"github.com/mark3labs/mcp-go/server"
)

func toolNamesFromServer(tools map[string]*server.ServerTool) []string {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func TestRegisterConfigTools_GetCurrentConfigDevOnly(t *testing.T) {
	t.Run("registered in development mode", func(t *testing.T) {
		ms := mockMcpServerWithConfig(MCPConfig{})
		ms.service.conf.Serv.Production = false
		ms.srv = server.NewMCPServer("test", "0.0.0")
		ms.registerConfigTools()

		if _, exists := ms.srv.ListTools()["get_current_config"]; !exists {
			t.Fatal("get_current_config should be registered in development mode")
		}
	})

	t.Run("not registered in production mode", func(t *testing.T) {
		ms := mockMcpServerWithConfig(MCPConfig{})
		ms.service.conf.Serv.Production = true
		ms.srv = server.NewMCPServer("test", "0.0.0")
		ms.registerConfigTools()

		if _, exists := ms.srv.ListTools()["get_current_config"]; exists {
			t.Fatal("get_current_config should not be registered in production mode")
		}
	})
}

func TestRegisterTools_QuickSetupNotRegistered(t *testing.T) {
	ms := mockMcpServerWithConfig(MCPConfig{
		AllowRawQueries:    true,
		EnableSearch:       true,
		AllowConfigUpdates: true,
		AllowSchemaReload:  true,
		AllowSchemaUpdates: true,
		AllowDevTools:      true,
	})
	ms.service.conf.Serv.Production = false
	ms.srv = server.NewMCPServer("test", "0.0.0")
	ms.registerTools()

	tools := ms.srv.ListTools()
	if _, exists := tools["quick_setup"]; exists {
		t.Fatal("quick_setup should not be registered")
	}
	if _, exists := tools["apply_database_setup"]; !exists {
		t.Fatal("apply_database_setup should still be registered")
	}
}

func TestMCPToolListMatchesRegisteredTools(t *testing.T) {
	testCases := []struct {
		name       string
		production bool
		cfg        MCPConfig
	}{
		{
			name:       "development all features enabled",
			production: false,
			cfg: MCPConfig{
				AllowRawQueries:    true,
				EnableSearch:       true,
				AllowConfigUpdates: true,
				AllowSchemaReload:  true,
				AllowSchemaUpdates: true,
				AllowDevTools:      true,
			},
		},
		{
			name:       "production all features enabled",
			production: true,
			cfg: MCPConfig{
				AllowRawQueries:    true,
				EnableSearch:       true,
				AllowConfigUpdates: true,
				AllowSchemaReload:  true,
				AllowSchemaUpdates: true,
				AllowDevTools:      true,
			},
		},
		{
			name:       "development minimal features",
			production: false,
			cfg: MCPConfig{
				EnableSearch: false,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			conf := &Config{Serv: Serv{Production: tc.production, MCP: tc.cfg}}
			expected := mcpToolList(conf)
			sort.Strings(expected)

			ms := mockMcpServerWithConfig(tc.cfg)
			ms.service.conf.Serv.Production = tc.production
			ms.srv = server.NewMCPServer("test", "0.0.0")
			ms.registerTools()

			actual := toolNamesFromServer(ms.srv.ListTools())
			if !reflect.DeepEqual(expected, actual) {
				t.Fatalf("mcpToolList mismatch\nexpected: %v\nactual:   %v", expected, actual)
			}
		})
	}
}
