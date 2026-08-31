package eval

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
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
	// Temperature and TopP pin how the model under evaluation samples.
	//
	// Unset leaves the stack default, which is greedy. Collecting several
	// samples of one task only produces something to select from when they
	// can differ, so a sampling run sets these and a benchmark does not.
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	// PinDataAnchor freezes the demo's date-relative seed data at one anchor
	// day. Demo dates shift forward on every boot so "today" questions keep
	// working, but that shift changes the dataset fingerprint — so a run that
	// outlives a UTC midnight could not be resumed the next morning, since its
	// completed episodes were graded against the previous day's data. Resuming
	// pins the anchor the run started on and the boot leaves the dates alone.
	PinDataAnchor string `json:"pin_data_anchor,omitempty"`
	// AgentTimeoutSeconds caps how long one episode's agent run may take.
	//
	// The stack default of 50 seconds is sized for a person waiting on a
	// request. An episode is a longer thing, and one whose completions come
	// from a trainer is longer still. Zero leaves the configured value alone.
	AgentTimeoutSeconds int `json:"agent_timeout_seconds,omitempty"`
	// FreezeTime fixes what the environment calls "now", as an RFC3339 instant.
	//
	// PinDataAnchor stops the seeded data from moving; this stops the questions
	// asked about it from moving. Both are needed for a run to mean the same
	// thing at any hour: a task that says "in the last 30 days" is answered
	// against a window whose end is the clock, so an unfrozen clock quietly
	// changes the question between one episode and the next. Setting it also
	// pins the data anchor to the same day unless PinDataAnchor says otherwise.
	FreezeTime string `json:"freeze_time,omitempty"`
}

// FrozenTime returns the fixed instant this environment runs at, and whether one
// was requested.
func (s EnvSpec) FrozenTime() (time.Time, bool, error) {
	value := strings.TrimSpace(s.FreezeTime)
	if value == "" {
		return time.Time{}, false, nil
	}
	frozen, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("freeze_time %q is not an RFC3339 instant: %w", value, err)
	}
	return frozen.UTC(), true, nil
}

// EffectiveDataAnchor returns the day the seeded data should be pinned to. A
// frozen clock implies its own day: seeding the data to one day and asking the
// questions from another would make every relative window off by the gap.
func (s EnvSpec) EffectiveDataAnchor() (string, error) {
	if anchor := strings.TrimSpace(s.PinDataAnchor); anchor != "" {
		return anchor, nil
	}
	frozen, ok, err := s.FrozenTime()
	if err != nil || !ok {
		return "", err
	}
	return frozen.Format("2006-01-02"), nil
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
