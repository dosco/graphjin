package serv

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

// watchSeedTimeout bounds the whole seeding pass. Each definition is validated
// with a live subscription probe, so a wedged source could otherwise hold up
// service startup.
const watchSeedTimeout = 60 * time.Second

// WatchSeed is one standing question a project boots with. The fields are
// exactly the gj_watch mutation input, so a seeded watch and a hand-written one
// are the same object created through the same path.
type WatchSeed struct {
	Name           string
	Description    string
	Query          string
	SavedQueryName string
	VariablesJSON  string
	DeliveryJSON   string
	AbsenceJSON    string
	EnrichJSON     string
}

// OperatorSeed declares the local development operator a zero-configuration
// project boots as: the owner of everything the project seeds, and the identity
// the console offers when the browser has none.
type OperatorSeed struct {
	UserID string
	// Role is the role seeded watches run under. Watch subscriptions execute
	// with the owner's stored role, so this is what decides what a standing
	// question can see — keep it least-privilege.
	Role string
	// ConsoleRole is the role the console adopts, when it differs from Role.
	// Reading the control-plane roots the console renders needs wider access
	// than running a watch does, so the two are allowed to diverge rather than
	// forcing watches to run with console-shaped privileges.
	ConsoleRole string
	AccountID   string
	Watches     []WatchSeed

	// Report receives one line per watch so the caller can render progress in
	// its own status stream. It is never called with an error the caller is
	// expected to handle: seeding is best-effort by design.
	Report func(name, status, message string)
}

// OptionSetOperatorSeed registers standing watches to create once, after the
// engine is up and before the watch runner starts. Seeding failures are
// reported and skipped; they never fail service startup.
func OptionSetOperatorSeed(seed OperatorSeed) Option {
	return func(s *graphjinService) error {
		if strings.TrimSpace(seed.UserID) == "" {
			return errors.New("operator seed requires a user id")
		}
		s.operatorSeed = &seed
		return nil
	}
}

// operatorSeedRole is the role seeded watches run under when the seed does not
// name one. Watch subscriptions execute with the owner's stored role, so this
// is a real permission choice and not a label.
func (seed *OperatorSeed) operatorSeedRole() string {
	if role := strings.TrimSpace(seed.Role); role != "" {
		return role
	}
	return "user"
}

// consoleSeedRole is the role the console should adopt for this operator.
func (seed *OperatorSeed) consoleSeedRole() string {
	if role := strings.TrimSpace(seed.ConsoleRole); role != "" {
		return role
	}
	return seed.operatorSeedRole()
}

// seedOperatorWatches creates the project's declared standing questions. It is
// called after the artifact store exists and before the watch runner starts, so
// the runner's first reconcile already sees them.
//
// Watches are created if absent rather than upserted: an upsert would clear the
// cursor checkpoint and re-fire every seeded watch on every boot, and would
// resurrect a watch the operator deliberately deleted.
func (s *graphjinService) seedOperatorWatches(parent context.Context) {
	seed := s.operatorSeed
	if seed == nil || len(seed.Watches) == 0 {
		return
	}
	report := seed.Report
	if report == nil {
		report = func(string, string, string) {}
	}

	switch {
	case s.gj == nil:
		report("", "skipped", "query engine unavailable")
		return
	case !s.watchesEnabled():
		report("", "skipped", "watches are disabled")
		return
	}
	if _, _, _, _, ok := s.watchDB(); !ok {
		report("", "skipped", "watch store is not configured")
		return
	}

	ctx, cancel := context.WithTimeout(parent, watchSeedTimeout)
	defer cancel()

	ownerID := strings.TrimSpace(seed.UserID)
	// A plain owner context, not internalStoreContext: upsertWatch stamps
	// owner_id from the caller, and the store-role escalation happens inside
	// the store mutation itself.
	ownerCtx := s.ownerContext(ctx, ownerID, seed.operatorSeedRole(), seed.AccountID)
	cp := newWatchControlPlane(s)

	for _, w := range seed.Watches {
		name := strings.TrimSpace(w.Name)
		if name == "" {
			report("", "failed", "watch seed requires a name")
			continue
		}
		existing, err := s.internalWatchStoreRow(ownerCtx, watchID(ownerID, name))
		if err != nil {
			s.logWatchSeedError(name, err)
			report(name, "failed", err.Error())
			continue
		}
		if existing != nil {
			report(name, "present", "already registered")
			continue
		}
		if _, err := cp.mutateRow(ownerCtx, core.ManagedMutationRoot{
			Table:     watchesRootTable,
			Operation: "insert",
			Input:     w.mutationInput(name),
		}); err != nil {
			s.logWatchSeedError(name, err)
			report(name, "failed", err.Error())
			continue
		}
		report(name, "created", w.Description)
	}
}

func (s *graphjinService) logWatchSeedError(name string, err error) {
	if s.log != nil {
		s.log.Warnf("watch seed %q: %s", name, err)
	}
}

func (w WatchSeed) mutationInput(name string) map[string]any {
	input := map[string]any{"name": name}
	for key, value := range map[string]string{
		"description":      w.Description,
		"query":            w.Query,
		"saved_query_name": w.SavedQueryName,
		"variables_json":   w.VariablesJSON,
		"delivery_json":    w.DeliveryJSON,
		"absence_json":     w.AbsenceJSON,
		"enrich_json":      w.EnrichJSON,
	} {
		if strings.TrimSpace(value) != "" {
			input[key] = value
		}
	}
	return input
}
