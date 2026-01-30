package serv

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var version string

const (
	serverName = "GraphJin"
	defaultHP  = "0.0.0.0:8080"
)

// Initialize the watcher for the graphjin config file
func initConfigWatcher(s1 *HttpService) {
	s := s1.Load().(*graphjinService)
	if s.conf.Serv.Production {
		return
	}

	go func() {
		err := startConfigWatcher(s1)
		if err != nil {
			s.log.Fatalf("error in config file watcher: %s", err)
		}
	}()
}

// Initialize the hot deploy watcher
func initHotDeployWatcher(s1 *HttpService) {
	s := s1.Load().(*graphjinService)
	go func() {
		err := startHotDeployWatcher(s1)
		if err != nil {
			s.log.Fatalf("error in hot deploy watcher: %s", err)
		}
	}()
}

// Start the HTTP server
func startHTTP(s1 *HttpService) {
	s := s1.Load().(*graphjinService)

	r := chi.NewRouter()
	routes, err := routesHandler(s1, r, s.namespace)
	if err != nil {
		s.log.Fatalf("error setting up routes: %s", err)
	}

	s.srv = &http.Server{
		Addr:              s.conf.hostPort,
		Handler:           routes,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		MaxHeaderBytes:    1 << 20,
		ReadHeaderTimeout: 10 * time.Second,
	}

	idleConnsClosed := make(chan struct{})
	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt)
		<-sigint

		if err := s.srv.Shutdown(context.Background()); err != nil {
			s.log.Warn("shutdown signal received")
		}
		close(idleConnsClosed)
	}()

	s.srv.RegisterOnShutdown(func() {
		if s.closeFn != nil {
			s.closeFn()
		}
		if s.cache != nil {
			s.cache.Close() //nolint:errcheck
		}
		if s.db != nil {
			s.db.Close() //nolint:errcheck
		}
		s.log.Info("shutdown complete")
	})

	ver := version
	dep := s.conf.name

	if ver == "" {
		ver = "not-set"
	}

	fields := []zapcore.Field{
		zap.String("version", ver),
		zap.String("host-port", s.conf.hostPort),
		zap.String("app-name", s.conf.AppName),
		zap.String("env", os.Getenv("GO_ENV")),
		zap.Bool("hot-deploy", s.conf.HotDeploy),
		zap.Bool("production", s.conf.Core.Production),
	}

	if s.namespace != nil {
		fields = append(fields, zap.String("namespace", *s.namespace))
	}

	if s.conf.HotDeploy {
		fields = append(fields, zap.String("deployment-name", dep))
	}

	s.zlog.Info("GraphJin started", fields...)
	printDevModeInfo(s)

	l, err := net.Listen("tcp", s.conf.hostPort)
	if err != nil {
		s.log.Fatalf("failed to init port: %s", err)
	}

	// signal we are open for business.
	s.state = servListening

	if err := s.srv.Serve(l); err != http.ErrServerClosed {
		s.log.Fatalf("failed to start: %s", err)
	}
	<-idleConnsClosed
}

// Set the server header
func setServerHeader(h http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", serverName)
		h.ServeHTTP(w, r)
	}
	return http.HandlerFunc(fn)
}

// printDevModeInfo prints useful development information on startup
func printDevModeInfo(s *graphjinService) {
	if s.conf.Serv.Production {
		return
	}

	// Convert 0.0.0.0 to localhost for display
	hostPort := s.conf.hostPort
	displayHost := hostPort
	if strings.HasPrefix(hostPort, "0.0.0.0:") {
		displayHost = "localhost" + hostPort[7:]
	}

	fmt.Println()
	fmt.Println("Development Server URLs")
	fmt.Println("───────────────────────")

	if s.conf.WebUI && !s.conf.MCP.Only {
		fmt.Printf("  Web UI:      http://%s/\n", displayHost)
	}
	if !s.conf.MCP.Only {
		fmt.Printf("  GraphQL:     http://%s/api/v1/graphql\n", displayHost)
		fmt.Printf("  REST API:    http://%s/api/v1/rest/\n", displayHost)
	}
	if !s.conf.MCP.Disable {
		fmt.Printf("  MCP (SSE):   http://%s/api/v1/mcp\n", displayHost)
	}

	if !s.conf.MCP.Disable {
		fmt.Println()
		fmt.Println("Claude Desktop Configuration")
		fmt.Println("────────────────────────────")
		fmt.Println("Add to claude_desktop_config.json:")
		fmt.Println()
		printClaudeConfig(s.conf)
	}
	fmt.Println()
}

// printClaudeConfig prints a Claude Desktop configuration snippet
func printClaudeConfig(conf *Config) {
	execPath, _ := os.Executable()
	if execPath == "" {
		execPath = "graphjin"
	}

	configPath := conf.ConfigPath
	if configPath == "" {
		configPath = "./config"
	}

	userID := conf.MCP.StdioUserID
	if userID == "" {
		userID = "1"
	}
	userRole := conf.MCP.StdioUserRole
	if userRole == "" {
		userRole = "user"
	}

	fmt.Printf(`  {
    "mcpServers": {
      "%s": {
        "command": "%s",
        "args": ["mcp", "--path", "%s"],
        "env": {
          "GRAPHJIN_USER_ID": "%s",
          "GRAPHJIN_USER_ROLE": "%s"
        }
      }
    }
  }
`, conf.AppName, execPath, configPath, userID, userRole)
}
