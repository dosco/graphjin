package agent

import (
	"context"
	"regexp"
	"sort"
	"strings"

	ax "github.com/ax-llm/ax/packages/go"
)

// A turn nobody is watching proposes a change by stating it: its reply says
// whether a change is called for (proposes_change) and what the change is
// (change_request), and GraphJin sends that to the data agent, which holds it
// for approval. Why the reply and not the turn's code is in
// leadUnattendedChangeRules.
//
// The reply decides from what the turn found, so GraphJin makes the hand-off
// to it (handDecision): the turn's own record of what its functions returned
// (turnRecord), and nothing the code made of that. Ax has the reply stage
// follow the task and evidence the code ends on, and the code ends on a
// decision it made without reading. In all 45 look-up turns measured after
// the code had been told not to decide, its hand-off still carried a decision,
// reached by matching words: with both answers saying yes, code that tested
// status === 'ok' ended on "No change is needed" and the reply wrote "its
// account is at churn risk and it has 2 failed invoices, but no change was
// proposed for this turn"; with both answers saying no, code that searched
// them for "churn risk" and "failed invoice" passed proposes_change: true,
// and the reply proposed the change. Handing the code's conclusion over as
// notes, marked as written before the answers were read, did not stop the
// second.

const (
	// replyAnswersKey is where a turn's data-agent answers go in the record
	// handed to the reply stage.
	replyAnswersKey = "data_agent_answers"
	// replySkillsKey is where the skills the turn loaded go, and
	// replyCheckBacksKey the check-backs it scheduled.
	replySkillsKey     = "loaded_skills"
	replyCheckBacksKey = "check_backs_scheduled"
	// replyNothingAsked stands for the answers when the turn asked nothing: a
	// reply handed only the instruction answered "no change is needed" about
	// facts nobody had looked up.
	replyNothingAsked = "none: the data agent was asked nothing this turn, so nothing beyond the attached rows was looked up"
	// leadDecisionTask is the task the reply stage is given on such a turn.
	leadDecisionTask = "Write the reply to the instruction from what this turn found: " + replyAnswersKey + " is what the data agent answered, and the rows attached to the turn are data too. Decide from them whether the instruction calls for a change."
)

// turnRecord is what the turn's functions returned, as the reply stage of a
// deciding turn reads it: each question with how it ended and what came
// back, the skills the turn loaded, and the check-backs it scheduled.
func (r *leadRun) turnRecord() map[string]any {
	record := map[string]any{replyAnswersKey: replyNothingAsked}
	if results := r.results(); len(results) != 0 {
		answers := make([]any, 0, len(results))
		for _, d := range results {
			answers = append(answers, d.modelView())
		}
		record[replyAnswersKey] = answers
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.loadedSkillText) != 0 {
		record[replySkillsKey] = append([]any(nil), r.loadedSkillText...)
	}
	if len(r.followUps) != 0 {
		notes := make([]string, 0, len(r.followUps))
		for note := range r.followUps {
			notes = append(notes, note)
		}
		sort.Strings(notes)
		scheduled := make([]any, 0, len(notes))
		for _, note := range notes {
			scheduled = append(scheduled, map[string]any{"at": r.followUps[note].At, "note": note})
		}
		record[replyCheckBacksKey] = scheduled
	}
	return record
}

// handDecision makes the hand-off of a step that ends the stage with final()
// or respond(): the reply stage is given the task of deciding and the turn's
// record, in place of the task and evidence the code ended on.
func handDecision(result ax.Value, record map[string]any) {
	if step := stageEnd(result); step != nil {
		step["args"] = []any{leadDecisionTask, record}
	}
}

// stageEnd returns the completion of a step that ends its stage with final()
// or respond(), or nil.
func stageEnd(result ax.Value) map[string]any {
	step := mapValue(result)
	if step == nil {
		return nil
	}
	if payload, ok := step["completion_payload"].(map[string]any); ok {
		step = payload
	}
	switch strings.ToLower(stringValue(step["type"])) {
	case "final", "respond":
		return step
	}
	return nil
}

func endsStage(result ax.Value) bool { return stageEnd(result) != nil }

var (
	discoverSkillsCall = regexp.MustCompile(`(?s)\bdiscover\s*\(\s*\{[^}]*?\bskills\s*:\s*\[(.*?)\]`)
	quotedSkillName    = regexp.MustCompile("\"([^\"\\\\]+)\"|'([^'\\\\]+)'|`([^`$\\\\]+)`")
)

// discoveredSkills returns the skill names a step's code passes to
// discover({skills: [...]}) as plain string literals.
func discoveredSkills(code string) []ax.Value {
	var names []ax.Value
	for _, call := range discoverSkillsCall.FindAllStringSubmatch(code, -1) {
		for _, quoted := range quotedSkillName.FindAllStringSubmatch(call[1], -1) {
			for _, name := range quoted[1:] {
				if name = strings.TrimSpace(name); name != "" {
					names = append(names, name)
				}
			}
		}
	}
	return names
}

// proposeStated sends the change an unattended reply states to the data
// agent and returns that delegation. It returns nil when the reply states no
// change, or when a change this turn asked for is already held: a reply
// carries one change.
func (r *leadRun) proposeStated(ctx context.Context, out ax.Value) *leadDelegation {
	fields := mapValue(normalizeValue(out))
	if proposes, _ := fields["proposes_change"].(bool); !proposes {
		return nil
	}
	request := strings.TrimSpace(stringValue(fields["change_request"]))
	if request == "" || r.readOnly || r.delegatedApproval() != nil {
		return nil
	}
	key := leadQuestionKey(request)
	r.mu.Lock()
	if prior, ok := r.answered[key]; ok {
		// The turn's code asked for this in the same words and nothing was
		// held: asking again would run the same request twice.
		r.mu.Unlock()
		return &prior
	}
	r.asked++
	index := r.asked
	r.mu.Unlock()

	d := r.runOne(ctx, index, request, false)
	r.mu.Lock()
	r.delegations = append(r.delegations, d)
	if d.err == nil {
		r.answered[key] = d
	}
	r.mu.Unlock()
	return &d
}

// notHeldSentence says, for a reply that proposed a change, that the data
// agent did not hold it and what the data agent said instead.
func (d leadDelegation) notHeldSentence() string {
	said := strings.TrimSpace(d.resp.Answer)
	switch {
	case d.err != nil:
		said = d.err.Error()
	case said == "" && len(d.resp.Errors) != 0:
		said = d.resp.Errors[0].Message
	}
	sentence := "GraphJin sent this change to the data agent and it is not held for approval."
	if said != "" {
		sentence += " The data agent replied: " + truncateString(said, 600)
	}
	return sentence
}
