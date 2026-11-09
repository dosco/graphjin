package agent

import (
	"fmt"
	"strings"
)

// Autonomy rules let some held writes run without waiting for the user. A
// write the agent proposes is dry-run first; when every action it takes on its
// table is covered by a rule above ask, and the dry run changed no more rows
// than the rule allows, GraphJin runs it at once instead of holding it.
// Anything without a rule waits for the user, as it always has.

const (
	// AutonomyAsk holds the write for the user's approval (the default).
	AutonomyAsk = "ask"
	// AutonomyNotify runs the write, then tells its owner.
	AutonomyNotify = "notify"
	// AutonomyAuto runs the write and records it.
	AutonomyAuto = "auto"
)

// AutonomyRule is one row of agent.autonomy.
type AutonomyRule struct {
	// Action is data.insert, data.update or data.delete.
	Action string `mapstructure:"action" json:"action" jsonschema:"enum=data.insert,enum=data.update,enum=data.delete"`
	// Table is the table the write changes, or * for any table.
	Table string `mapstructure:"table" json:"table"`
	// Level is ask, notify or auto.
	Level string `mapstructure:"level" json:"level" jsonschema:"enum=ask,enum=notify,enum=auto"`
	// MaxRows caps the rows the dry run may change for the rule to apply;
	// zero means no cap.
	MaxRows int `mapstructure:"max_rows" json:"max_rows,omitempty"`
}

var autonomyRank = map[string]int{AutonomyAsk: 0, AutonomyNotify: 1, AutonomyAuto: 2}

// ValidateAutonomy checks agent.autonomy.
func ValidateAutonomy(rules []AutonomyRule) error {
	for i, rule := range rules {
		action := strings.TrimSpace(rule.Action)
		switch action {
		case CapabilityActionDataInsert, CapabilityActionDataUpdate, CapabilityActionDataDelete:
		case CapabilityActionAPIWrite, CapabilityActionAPIDelete:
			return fmt.Errorf("agent.autonomy[%d]: %s cannot be automatic: an API call cannot be tried in a rolled-back transaction first, so it always waits for approval", i, action)
		default:
			return fmt.Errorf("agent.autonomy[%d].action must be data.insert, data.update or data.delete, got %q", i, rule.Action)
		}
		if strings.TrimSpace(rule.Table) == "" {
			return fmt.Errorf("agent.autonomy[%d].table is required; use * for any table", i)
		}
		if _, ok := autonomyRank[strings.TrimSpace(rule.Level)]; !ok {
			return fmt.Errorf("agent.autonomy[%d].level must be ask, notify or auto, got %q", i, rule.Level)
		}
		if rule.MaxRows < 0 {
			return fmt.Errorf("agent.autonomy[%d].max_rows must not be negative", i)
		}
	}
	return nil
}

// AutonomyDecision is how a dry-run write is handled.
type AutonomyDecision struct {
	Level string
	// Rule names the rules that allowed it, such as
	// autonomy:data.update:invoices:notify; empty when it asks.
	Rule string
}

// ResolveAutonomy decides a write that took actions on table and changed rows
// rows in its dry run. Every action needs a rule above ask (a rule naming the
// table wins over *), each rule's max_rows must hold, and the least
// autonomous rule decides.
func ResolveAutonomy(rules []AutonomyRule, actions []string, table string, rows int) AutonomyDecision {
	ask := AutonomyDecision{Level: AutonomyAsk}
	table = strings.ToLower(strings.TrimSpace(table))
	if len(rules) == 0 || len(actions) == 0 || table == "" {
		return ask
	}
	level := AutonomyAuto
	var names []string
	for _, action := range actions {
		rule, ok := autonomyRuleFor(rules, action, table)
		if !ok || rule.Level == AutonomyAsk || (rule.MaxRows > 0 && rows > rule.MaxRows) {
			return ask
		}
		if autonomyRank[rule.Level] < autonomyRank[level] {
			level = rule.Level
		}
		names = append(names, fmt.Sprintf("autonomy:%s:%s:%s", rule.Action, rule.Table, rule.Level))
	}
	return AutonomyDecision{Level: level, Rule: strings.Join(names, ",")}
}

func autonomyRuleFor(rules []AutonomyRule, action, table string) (AutonomyRule, bool) {
	var wildcard *AutonomyRule
	for i := range rules {
		rule := rules[i]
		if strings.TrimSpace(rule.Action) != action {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(rule.Table)) {
		case table:
			return rule, true
		case "*":
			if wildcard == nil {
				wildcard = &rules[i]
			}
		}
	}
	if wildcard != nil {
		return *wildcard, true
	}
	return AutonomyRule{}, false
}

// BudgetConfig caps model tokens over a rolling 24 hours. Unset, budgets are
// on in agentic mode with the default caps and off elsewhere.
type BudgetConfig struct {
	// Enabled turns budgets on or off; unset follows the mode.
	Enabled *bool `mapstructure:"enabled" json:"enabled,omitempty"`
	// UserDailyTokens caps one person's agent runs, including runs made on
	// their behalf. Zero means the default (20,000,000, about 280 lead turns).
	UserDailyTokens int64 `mapstructure:"user_daily_tokens" json:"user_daily_tokens,omitempty" jsonschema:"default=20000000"`
	// RoutineDailyTokens caps one watch routine, its continuations included.
	// Zero means the default (2,000,000).
	RoutineDailyTokens int64 `mapstructure:"routine_daily_tokens" json:"routine_daily_tokens,omitempty" jsonschema:"default=2000000"`
}

// Validate checks agent.budgets.
func (b BudgetConfig) Validate() error {
	if b.UserDailyTokens < 0 || b.RoutineDailyTokens < 0 {
		return fmt.Errorf("agent.budgets caps must not be negative")
	}
	return nil
}

// ServiceIdentityConfig is agent.service_identity: a user id and a configured
// role an organization agent runs as.
type ServiceIdentityConfig struct {
	UserID string `mapstructure:"user_id" json:"user_id,omitempty"`
	Role   string `mapstructure:"role" json:"role,omitempty"`
}

// Configured reports whether a service identity is set.
func (c ServiceIdentityConfig) Configured() bool {
	return strings.TrimSpace(c.UserID) != "" && strings.TrimSpace(c.Role) != ""
}
