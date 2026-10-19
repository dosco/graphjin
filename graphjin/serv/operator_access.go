package serv

import (
	"github.com/dosco/graphjin/core/v3/featurecap"
)

// operatorAccessAllowed reports whether a role may use operator-only HTTP
// surfaces such as evaluation reports and scoring: it must be able to read a
// control-plane root (gj_runtime, gj_security, gj_config), or the deployment
// must enable dev tools or raw GraphQL reads.
func operatorAccessAllowed(conf *Config, role string) bool {
	for _, root := range registeredSystemRoots(conf) {
		switch root {
		case "gj_runtime", "gj_security", "gj_config":
			if systemRootAllowed(conf, root, systemActionRead, role) {
				return true
			}
		}
	}
	if conf == nil {
		return false
	}
	return conf.effectiveFeatureCapability(featurecap.KindSystem, featurecap.KeyDevToolsRead) ||
		conf.effectiveFeatureCapability(featurecap.KindSystem, featurecap.KeyRawGraphQLQuery)
}
