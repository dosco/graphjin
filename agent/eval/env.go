package eval

import (
	"context"
	"errors"
)

type Target string

const (
	TargetLocal  Target = "local"
	TargetDemo   Target = "demo"
	TargetRemote Target = "remote"
)

type EnvSpec struct {
	Target     Target `json:"target"`
	ConfigPath string `json:"config_path,omitempty"`
	Anchor     string `json:"anchor,omitempty"`
	Seed       int64  `json:"seed,omitempty"`
	Writable   bool   `json:"writable,omitempty"`
	Reactive   bool   `json:"reactive,omitempty"`
	Resettable bool   `json:"resettable,omitempty"`
}

type DatasetFingerprint struct {
	DataAnchor       string `json:"data_anchor,omitempty"`
	SeedManifestHash string `json:"seed_manifest_hash,omitempty"`
	CatalogHash      string `json:"catalog_hash"`
}

func (f DatasetFingerprint) Equal(other DatasetFingerprint) bool {
	// A catalog hash alone does not identify live row values. Value-correctness
	// baselines are comparable only for datasets with both an explicit anchor
	// and a deterministic seed/provisioning manifest. Runner-level aggregate
	// oracle hashes provide the stable-target alternative.
	if f.DataAnchor == "" || f.SeedManifestHash == "" || other.DataAnchor == "" || other.SeedManifestHash == "" {
		return false
	}
	return f.DataAnchor == other.DataAnchor &&
		f.SeedManifestHash == other.SeedManifestHash &&
		f.CatalogHash == other.CatalogHash
}

type Instance interface {
	BaseURL() string
	Headers() map[string]string
	Fingerprint() DatasetFingerprint
	Label() string
	Close() error
}

type Env interface {
	Start(context.Context, EnvSpec) (Instance, error)
}

// InstancePool is the v1 seam for parallel, resettable v2 rollout workers.
// V1 runners intentionally acquire a single instance and execute sequentially.
type InstancePool interface {
	Acquire(context.Context) (Instance, error)
	Release(Instance) error
	Close() error
}

// ResettableInstance is the reserved v2 environment boundary. SQLite-backed
// implementations can restore a copied database file; PostgreSQL-backed
// implementations can restore a template database. V1 environments do not
// implement or invoke Reset because mutation tasks are rejected by the schema.
type ResettableInstance interface {
	Instance
	Reset(context.Context) error
}

type ResettableStaticInstance struct {
	*StaticInstance
	ResetFunc func(context.Context) error
}

func (i *ResettableStaticInstance) Reset(ctx context.Context) error {
	if i == nil || i.ResetFunc == nil {
		return errors.New("evaluation instance does not define reset")
	}
	return i.ResetFunc(ctx)
}

type StaticInstance struct {
	URL            string
	RequestHeaders map[string]string
	Dataset        DatasetFingerprint
	TargetLabel    string
	CloseFunc      func() error
}

func (i *StaticInstance) BaseURL() string                 { return i.URL }
func (i *StaticInstance) Headers() map[string]string      { return cloneStrings(i.RequestHeaders) }
func (i *StaticInstance) Fingerprint() DatasetFingerprint { return i.Dataset }
func (i *StaticInstance) Label() string                   { return i.TargetLabel }
func (i *StaticInstance) Close() error {
	if i.CloseFunc != nil {
		return i.CloseFunc()
	}
	return nil
}

func cloneStrings(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
