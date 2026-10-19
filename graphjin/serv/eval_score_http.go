package serv

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	gjagent "github.com/dosco/graphjin/agent/v3"
	gjeval "github.com/dosco/graphjin/agent/v3/eval"
	"github.com/dosco/graphjin/auth/v3"
)

// The scoring endpoint exists so a trainer running its own rollout loop grades
// episodes with the same function the harness uses, rather than reimplementing
// the contract and slowly diverging from it. Two rewards that disagree about
// the same episode are worse than no reward at all: the disagreement shows up
// as a model that improves on one number and not the other.
type evalScoreRequest struct {
	Task     gjeval.Task      `json:"task"`
	Response gjagent.Response `json:"response"`
	// Oracle is the ground truth the answer is graded against, resolved by the
	// caller. Scoring stays a pure function of what it is given: a server that
	// re-ran the oracle itself would grade against a database that has moved
	// since the episode, and the same episode would score differently on replay.
	Oracle    *gjeval.OracleResult `json:"oracle,omitempty"`
	LatencyMS int64                `json:"latency_ms,omitempty"`
	Profile   string               `json:"reward_profile,omitempty"`
	// Mutation carries what the environment observed about a write. A write is
	// graded by the state the database ended in, which the caller has to
	// observe; there is nothing in the response that can stand in for it.
	Mutation *gjeval.MutationOutcome `json:"mutation,omitempty"`
}

type evalScoreResponse struct {
	Score         gjeval.ScoreDetail `json:"score"`
	Reward        float64            `json:"reward"`
	RewardProfile string             `json:"reward_profile"`
	RewardVersion string             `json:"reward_version"`
	OracleValue   string             `json:"oracle_value,omitempty"`
}

func (s *HttpService) EvalScore(ah auth.HandlerFunc) http.Handler {
	return apiV1Handler(s, nil, s.apiV1EvalScore(nil), ah)
}

func (s *HttpService) EvalScoreWithNS(ah auth.HandlerFunc, ns string) http.Handler {
	return apiV1Handler(s, &ns, s.apiV1EvalScore(&ns), ah)
}

func (s1 *HttpService) apiV1EvalScore(_ *string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed", "message": "Only POST is supported."})
			return
		}
		s := s1.Load().(*graphjinService)
		if !evalReportsConfigured(s.conf) {
			writeJSONStatus(w, http.StatusNotFound, map[string]any{"error": "not_found", "message": "Evaluation scoring is unavailable in this mode."})
			return
		}
		ctx := s.applyIdentityContext(r.Context())
		roots := consoleReadableRoots(s.conf, runtimeRoleClass(ctx))
		features := consoleEnabledFeatures(s.conf)
		if !consoleAdminWorkspaceAvailable(roots, features) {
			writeJSONStatus(w, http.StatusForbidden, map[string]any{"error": "operator_access_required", "message": "Scoring requires operator access to the GraphJin console."})
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		if err != nil {
			writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "unreadable_body", "message": err.Error()})
			return
		}
		var request evalScoreRequest
		if err := json.Unmarshal(body, &request); err != nil {
			writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid_body", "message": err.Error()})
			return
		}
		if err := request.Task.Normalize(); err != nil {
			writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid_task", "message": err.Error()})
			return
		}
		profile := gjeval.RewardProfile(strings.TrimSpace(request.Profile))
		if err := profile.Validate(); err != nil {
			writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid_reward_profile", "message": err.Error()})
			return
		}

		oracle := request.Oracle
		oracleValue := ""
		if oracle != nil {
			oracleValue = oracle.Value
		}

		detail := gjeval.ScoreWithProfile(request.Task, oracle, request.Response, request.LatencyMS, profile)
		if request.Mutation != nil {
			detail = gjeval.ScoreMutationWithProfile(detail, *request.Mutation, request.Response, profile)
		}
		writeJSONStatus(w, http.StatusOK, evalScoreResponse{
			Score: detail, Reward: detail.Vector.Reward,
			RewardProfile: string(profile), RewardVersion: gjeval.RewardVersion,
			OracleValue: oracleValue,
		})
	})
}
