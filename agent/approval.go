package agent

import (
	"context"
	"fmt"
	"strings"
)

// Write approvals. When a deployment requires them, the data agent never runs
// an application-data or API write itself: at the single point where a
// mutation would reach the engine it asks the runtime to hold the write as a
// proposal, and the reply comes back needs_approval carrying that proposal.
// The user approves or rejects it later, outside any agent run, and only the
// exact query the proposal pinned can then run. Watches, tasks and other
// GraphJin control-plane writes keep their own review flows and are not held.

const (
	// StatusNeedsApproval is a reply whose write is held for the user's approval.
	StatusNeedsApproval = "needs_approval"

	// WriteApprovalRequired holds every agent data write for approval.
	WriteApprovalRequired = "required"
	// WriteApprovalOff lets the agent run data writes directly.
	WriteApprovalOff = "off"

	// systemRootApproval is where proposals live. The agent can never write it:
	// a run that could approve its own proposal would approve nothing.
	systemRootApproval = "gj_approval"
)

// Approval is a held write as the caller sees it.
type Approval struct {
	ID      string `json:"id"`
	Hash    string `json:"hash"`
	Summary string `json:"summary"`
	// Query and Variables are exactly what runs once approved.
	Query     string `json:"query"`
	Variables any    `json:"variables,omitempty"`
	// Preview is what the write would change, from a dry run that was rolled
	// back; Preview["available"] is false when the source cannot dry-run.
	Preview   map[string]any `json:"preview,omitempty"`
	ExpiresAt string         `json:"expires_at,omitempty"`
}

// WriteProposer is implemented by a runtime that can hold a write for the
// user's approval. args carries the final query, variables and namespace.
//
// ProposeWrite dry-runs the write and rolls it back. dryRun is that
// execution's result in ExecuteGraphQL's shape, so a write that would fail
// fails exactly as it would have, with every recovery intact. When the dry
// run succeeded, approval is the stored proposal. A runtime without this
// method cannot hold writes, so required approvals refuse the write rather
// than run it.
type WriteProposer interface {
	ProposeWrite(ctx context.Context, args map[string]any) (dryRun any, approval *Approval, err error)
}

// ValidateWriteApproval rejects a write_approval setting outside the
// vocabulary. Empty leaves the choice to the service.
func ValidateWriteApproval(value string) error {
	switch strings.TrimSpace(value) {
	case "", WriteApprovalRequired, WriteApprovalOff:
		return nil
	}
	return fmt.Errorf("agent.write_approval must be %q or %q, got %q", WriteApprovalRequired, WriteApprovalOff, value)
}

// heldWrite reports whether a mutation is an application-data or API write
// that approvals hold: every root is outside the GraphJin control plane.
func heldWrite(query string) bool {
	if !ContainsMutationOperation(query) {
		return false
	}
	items := mutationRootActions(query)
	if len(items) == 0 {
		return false
	}
	for _, item := range items {
		action := capabilityActionForMutation(item.root, item.action)
		if !strings.HasPrefix(action, "data.") && !strings.HasPrefix(action, "api.") {
			return false
		}
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(item.root)), "gj_") {
			return false
		}
	}
	return true
}

// writesApprovalRoot reports whether a mutation touches gj_approval.
func writesApprovalRoot(query string) bool {
	if !ContainsMutationOperation(query) {
		return false
	}
	for _, root := range MutationRootFields(query) {
		if strings.EqualFold(strings.TrimSpace(root), systemRootApproval) {
			return true
		}
	}
	return false
}

// approvalAnswer is the engine's reply for a held write: a model's own
// wording could say the change was made.
func approvalAnswer(approval *Approval) string {
	summary := strings.TrimSpace(approval.Summary)
	if summary == "" {
		summary = "a change to your data"
	}
	return "This change needs your approval before it runs: " + summary + ". Nothing has been changed yet."
}

// heldWriteResult is what the model reads back for a held write.
func heldWriteResult(approval *Approval) executeResult {
	return executeResult{
		Data: map[string]any{"held_for_approval": map[string]any{
			"id": approval.ID, "summary": approval.Summary, "preview": approval.Preview,
		}},
		Guidance: "This write did NOT run: it is held for the user's approval as " + approval.ID +
			". Do not run it again and do not say it was done; finish now, saying the change awaits the user's approval.",
	}
}

// MutationActions lists the capability actions a mutation needs, such as
// data.update, so an approval can re-check them when the user decides.
func MutationActions(query string) []string {
	var out []string
	for _, item := range mutationRootActions(query) {
		if action := capabilityActionForMutation(item.root, item.action); action != "" {
			out = appendUniqueString(out, action)
		}
	}
	return out
}
