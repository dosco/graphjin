package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/serv/v3"
	"github.com/spf13/cobra"
)

var (
	testVerbose bool
	testJSON    bool
)

// TestResult holds the overall test results
type TestResult struct {
	Success  bool            `json:"success"`
	Services []ServiceStatus `json:"services"`
	Error    string          `json:"error,omitempty"`
	Duration string          `json:"duration"`
}

// ServiceStatus holds the status of a single service
type ServiceStatus struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Status  string `json:"status"`
	Latency string `json:"latency,omitempty"`
	Note    string `json:"note,omitempty"`
}

func testCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "test",
		Short: "Validate config and test connectivity to all services",
		Long: `Validate configuration and test connectivity to all configured services:
- Primary database
- All databases in multi-DB mode (conf.Databases)
- Redis cache (if configured)
- GraphJin core schema compilation

Exit codes:
  0 - All services validated successfully
  1 - Configuration or service connection failed`,
		Run: cmdTest,
	}
	c.Flags().BoolVarP(&testVerbose, "verbose", "v", false, "Show detailed output for each service")
	c.Flags().BoolVar(&testJSON, "json", false, "Output results in JSON format")
	return c
}

func cmdTest(cmd *cobra.Command, args []string) {
	startTime := time.Now()
	var services []ServiceStatus

	// Step 1: Load configuration
	setup(cpath)
	services = append(services, ServiceStatus{
		Name:   "config",
		Type:   "yaml",
		Status: "ok",
	})

	// Step 2: Initialize GraphJin service (tests primary DB, Redis, core)
	service, err := serv.NewGraphJinService(conf)
	if err != nil {
		outputFailure(err, services, startTime)
		os.Exit(1)
	}

	// Primary database OK
	services = append(services, ServiceStatus{
		Name:   "database",
		Type:   conf.DB.Type,
		Status: "ok",
	})

	// Redis/cache status
	if conf.Redis.URL != "" {
		services = append(services, ServiceStatus{
			Name:   "cache",
			Type:   "redis",
			Status: "ok",
		})
	} else {
		services = append(services, ServiceStatus{
			Name:   "cache",
			Type:   "memory",
			Status: "ok",
			Note:   "in-memory fallback",
		})
	}

	// GraphJin core
	services = append(services, ServiceStatus{
		Name:   "graphjin",
		Type:   "core",
		Status: "ok",
	})

	// Step 3: Test additional databases (multi-DB mode)
	if isMultiDBMode() {
		dbResults, err := testMultiDBConnections()
		if err != nil {
			services = append(services, dbResults...)
			outputFailure(err, services, startTime)
			os.Exit(1)
		}
		services = append(services, dbResults...)
	}

	// All tests passed
	outputSuccess(services, startTime)

	// Keep service reference to avoid unused variable warning
	_ = service
}

// testMultiDBConnections tests all databases in conf.Databases
func testMultiDBConnections() ([]ServiceStatus, error) {
	var results []ServiceStatus
	fs := core.NewOsFS(cpath)

	for name, dbConf := range conf.Databases {
		start := time.Now()

		// Create temp config for this database
		tempConf := &serv.Config{}
		*tempConf = *conf
		tempConf.DB = serv.Database{
			Type:       dbConf.Type,
			Host:       dbConf.Host,
			Port:       uint16(dbConf.Port),
			User:       dbConf.User,
			Password:   dbConf.Password,
			DBName:     dbConf.DBName,
			ConnString: dbConf.ConnString,
		}

		conn, err := serv.NewDB(tempConf, true, log, fs)
		if err != nil {
			results = append(results, ServiceStatus{
				Name:   fmt.Sprintf("database:%s", name),
				Type:   dbConf.Type,
				Status: "failed",
				Note:   err.Error(),
			})
			return results, fmt.Errorf("database '%s': %w", name, err)
		}

		// Ping with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = conn.PingContext(ctx)
		cancel()
		conn.Close()

		if err != nil {
			results = append(results, ServiceStatus{
				Name:   fmt.Sprintf("database:%s", name),
				Type:   dbConf.Type,
				Status: "failed",
				Note:   err.Error(),
			})
			return results, fmt.Errorf("database '%s' ping: %w", name, err)
		}

		results = append(results, ServiceStatus{
			Name:    fmt.Sprintf("database:%s", name),
			Type:    dbConf.Type,
			Status:  "ok",
			Latency: time.Since(start).String(),
		})
	}

	return results, nil
}

func outputSuccess(services []ServiceStatus, start time.Time) {
	result := TestResult{
		Success:  true,
		Services: services,
		Duration: time.Since(start).String(),
	}
	outputResult(result)
}

func outputFailure(err error, services []ServiceStatus, start time.Time) {
	result := TestResult{
		Success:  false,
		Services: services,
		Error:    err.Error(),
		Duration: time.Since(start).String(),
	}
	outputResult(result)
}

func outputResult(result TestResult) {
	if testJSON {
		output, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(output))
		return
	}

	// Text output
	fmt.Println()
	for _, svc := range result.Services {
		status := "OK"
		if svc.Status == "failed" {
			status = "FAILED"
		}
		line := fmt.Sprintf("  %s (%s): %s", svc.Name, svc.Type, status)
		if svc.Latency != "" && testVerbose {
			line += fmt.Sprintf(" [%s]", svc.Latency)
		}
		if svc.Note != "" {
			if svc.Status == "failed" || testVerbose {
				line += fmt.Sprintf(" - %s", svc.Note)
			}
		}
		fmt.Println(line)
	}
	fmt.Println()

	if result.Success {
		fmt.Printf("All services validated (%s)\n", result.Duration)
	} else {
		fmt.Printf("Service validation failed: %s (%s)\n", result.Error, result.Duration)
	}
}
