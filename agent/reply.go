package agent

import (
	"errors"
	"fmt"
	"strings"

	ax "github.com/ax-llm/ax/packages/go"
)

// The reply contract: what every Response from Agent.Run or Lead.Run promises
// its caller, whichever agent produced it and however the run ended.
//
//   - Status is answered, needs_clarification, needs_approval, blocked or
//     error. A status a model invented outside that set is an error, never an
//     answer.
//   - A blocked reply carries a Refusal with a code, and only a blocked reply
//     carries one.
//   - An error reply carries at least one ErrorInfo.
//   - A needs_clarification reply carries its question as Answer and no errors.
//   - A needs_approval reply carries the held write as Approval, and only it
//     does; its answer states, in the engine's words, that nothing changed yet.
//
// The agents know different things about why a run was blocked: the worker
// knows its guard violations, the lead knows the refusals and failures of the
// questions it delegated. Each passes what it knows as replyFacts, and the
// rules above are applied here, once, for both. Before this file the lead
// re-implemented them one at a time, and each gap surfaced as a separate bug:
// a dropped refusal, a clarification reported as an error, a failure reported
// as a refusal.
type replyFacts struct {
	// refusal returns the refusal that stands behind a block, or nil.
	refusal func(Response) *Refusal
	// failures are errors from work that failed during the run. A block with
	// no refusal but with failures is reported as the error it was, so a
	// caller never retries a "refusal" that was really a timeout.
	failures []ErrorInfo
	// approval is a write the run held for the user's approval. Unless the
	// run was blocked, the reply is needs_approval carrying it.
	approval *Approval
}

// settleReply makes resp satisfy the reply contract.
func settleReply(resp Response, facts replyFacts) Response {
	resp = normalizeReplyStatus(resp)
	if facts.approval != nil && resp.Status != StatusBlocked {
		resp.Status = StatusNeedsApproval
		resp.Approval = facts.approval
	}
	if resp.Status == StatusNeedsApproval && resp.Approval == nil {
		resp.Status = StatusError
		resp.Errors = append(resp.Errors, ErrorInfo{
			Message:    "the reply says a change awaits approval but carries no proposal",
			Extensions: map[string]any{"code": "agent_approval_missing"},
		})
	}
	if resp.Status == StatusBlocked && resp.Refusal == nil && facts.refusal != nil {
		resp.Refusal = facts.refusal(resp)
	}
	if resp.Status == StatusBlocked && resp.Refusal == nil && len(facts.failures) != 0 {
		resp.Status = StatusError
		resp.Errors = append(resp.Errors, facts.failures...)
	}
	switch resp.Status {
	case StatusBlocked:
		if resp.Refusal == nil {
			resp.Refusal = unexplainedRefusal(resp)
		}
	case StatusError:
		resp.Refusal = nil
		if len(resp.Errors) == 0 {
			message := strings.TrimSpace(resp.Answer)
			if message == "" {
				message = "the agent run failed without reporting why"
			}
			resp.Errors = []ErrorInfo{{Message: message}}
		}
	case StatusNeedsClarification:
		resp.Refusal = nil
		resp.Errors = nil
	case StatusNeedsApproval:
		// The engine states what is pending: a model's wording could say the
		// change was made. An answer that already carries the engine's sentence
		// (the lead adds its own context around it) is kept.
		resp.Refusal = nil
		resp.Errors = nil
		if sentence := approvalAnswer(resp.Approval); !strings.Contains(resp.Answer, sentence) {
			resp.Answer = sentence
		}
	default:
		resp.Refusal = nil
	}
	if resp.Status != StatusNeedsApproval {
		resp.Approval = nil
	}
	return resp
}

// normalizeReplyStatus maps an empty status to answered and any status
// outside the contract to an error. It runs before the guards as well, so a
// model cannot slip an answer past them under an unknown status.
func normalizeReplyStatus(resp Response) Response {
	switch resp.Status {
	case StatusAnswered, StatusNeedsClarification, StatusBlocked, StatusError, StatusNeedsApproval:
	case "":
		resp.Status = StatusAnswered
	default:
		resp.Errors = append(resp.Errors, ErrorInfo{
			Message:    fmt.Sprintf("the agent replied with status %q, which is not answered, needs_clarification, needs_approval, blocked or error", resp.Status),
			Extensions: map[string]any{"code": "agent_status_invalid"},
		})
		resp.Status = StatusError
	}
	return resp
}

// unexplainedRefusal is the refusal for a block that no guard or delegated
// question explains: it keeps the code of the reply's own error when it has
// one (a model that emitted no runnable code), and is agent_blocked for a
// model that declined on its own.
func unexplainedRefusal(resp Response) *Refusal {
	because := strings.TrimSpace(resp.Answer)
	if because == "" && len(resp.Errors) != 0 {
		because = strings.TrimSpace(resp.Errors[0].Message)
	}
	if because == "" {
		because = "the agent declined the request"
	}
	code := firstErrorCode(resp.Errors)
	if code == "" {
		code = "agent_blocked"
	}
	return &Refusal{
		Code:              code,
		Because:           []string{because},
		LawfulAlternative: "Rephrase the request with the missing details, or ask an operator.",
		Retryable:         true,
	}
}

// firstErrorCode is the code of the first error that carries one.
func firstErrorCode(errors []ErrorInfo) string {
	for _, err := range errors {
		if code := strings.TrimSpace(stringFromMap(err.Extensions, "code")); code != "" {
			return code
		}
	}
	return ""
}

// forwardErrorReply is the reply for an Ax run that ended in an error instead
// of an output. A clarification the model asked for is a reply, not a
// failure; an exhausted step budget and a model that never emitted runnable
// code carry their own codes.
func forwardErrorReply(err error) Response {
	var axErr ax.AxError
	if errors.As(err, &axErr) && axErr.Category == "clarification" {
		return Response{Status: StatusNeedsClarification, Answer: clarificationAnswer(axErr.Payload)}
	}
	if isNoRuntimeCodeError(err) {
		return Response{
			Status: StatusBlocked,
			Answer: "I could not complete the GraphJin agent loop because the model did not emit a runnable action. No unsafe GraphJin execution was performed.",
			Errors: []ErrorInfo{{
				Message:    err.Error(),
				Extensions: map[string]any{"code": "agent_no_runtime_code", "protocol": "graphjin_discovery"},
			}},
		}
	}
	if isActorStepsExhaustedError(err) {
		return Response{Status: StatusError, Errors: []ErrorInfo{{
			Message:    "agent actor loop exceeded max steps",
			Extensions: map[string]any{"code": "agent_actor_steps_exhausted", "retryable": false},
		}}}
	}
	return Response{Status: StatusError, Errors: []ErrorInfo{PublicErrorInfo(err, "", "")}}
}
