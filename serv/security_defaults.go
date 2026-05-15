package serv

import (
	"strings"

	"github.com/dosco/graphjin/core/v3"
)

func controlPlaneTableReadOnly(conf *Config, database, table string) bool {
	if conf == nil {
		return true
	}
	if controlPlaneSourceReadOnly(conf, table) {
		return true
	}
	if readOnly, ok := configuredControlPlaneTableReadOnly(&conf.Core, database, table); ok {
		return readOnly
	}
	return defaultControlPlaneTableReadOnly(conf, table)
}

func configuredControlPlaneTableReadOnly(conf *core.Config, database, table string) (bool, bool) {
	if conf == nil {
		return false, false
	}
	for _, confTable := range conf.Tables {
		if confTable.Database != "" && database != "" && confTable.Database != database {
			continue
		}
		if strings.EqualFold(confTable.Name, table) || strings.EqualFold(confTable.Table, table) {
			return confTable.ReadOnly, true
		}
	}
	return false, false
}

func controlPlaneTableReadOnlyExplicit(conf *Config, database, table string) bool {
	if conf == nil {
		return false
	}
	if controlPlaneSourceReadOnly(conf, table) {
		return true
	}
	_, ok := configuredControlPlaneTableReadOnly(&conf.Core, database, table)
	return ok
}

func controlPlaneSourceReadOnly(conf *Config, table string) bool {
	switch strings.ToLower(strings.TrimSpace(table)) {
	case "gj_workflow", "gj_workflow_execution":
		return conf.workflowsSourceReadOnly()
	case "gj_config":
		return conf.graphjinSourceReadOnly()
	case "gj_catalog", "gj_security":
		return true
	default:
		return false
	}
}

func defaultControlPlaneTableReadOnly(conf *Config, table string) bool {
	mode := effectiveSecurityMode(conf)
	switch strings.ToLower(strings.TrimSpace(table)) {
	case "gj_workflow":
		return !defaultAllow(mode, true, false, false)
	case "gj_workflow_execution":
		return !defaultAllow(mode, true, false, true)
	case "gj_config":
		return !defaultAllow(mode, true, false, false)
	case "gj_catalog", "gj_security":
		return true
	default:
		return false
	}
}
