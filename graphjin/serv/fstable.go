package serv

import (
	"sync"

	"github.com/dosco/graphjin/core/v3"
)

// builtinFsBackends collects backend factories that this package
// registers via init() (s3, gcs). They're applied to every core engine
// the service constructs, so user code doesn't have to remember to
// pass options for the bundled backends.
//
// External callers can still register their own via the public
// core.OptionSetFilesystemBackend; that path is unchanged.
var (
	builtinFsBackendsMu sync.Mutex
	builtinFsBackends   = map[string]core.FilesystemBackendFactory{}
)

// registerBuiltinFilesystemBackend is invoked from this package's own
// init() blocks (one per build-tag-gated backend file). The map is
// drained into a slice of core.Option values by filesystemBackendOptions.
func registerBuiltinFilesystemBackend(name string, factory core.FilesystemBackendFactory) {
	builtinFsBackendsMu.Lock()
	defer builtinFsBackendsMu.Unlock()
	builtinFsBackends[name] = factory
}

// filesystemBackendOptions returns one core.Option per registered
// built-in backend, suitable for splatting into the core engine's
// option list during NewGraphJinService startup.
func filesystemBackendOptions() []core.Option {
	builtinFsBackendsMu.Lock()
	defer builtinFsBackendsMu.Unlock()
	out := make([]core.Option, 0, len(builtinFsBackends))
	for name, factory := range builtinFsBackends {
		out = append(out, core.OptionSetFilesystemBackend(name, factory))
	}
	return out
}
