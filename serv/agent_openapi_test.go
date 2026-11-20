package serv

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/invopop/jsonschema"
	schemavalidator "github.com/santhosh-tekuri/jsonschema/v6"
)

// The agent API's OpenAPI description is generated from the Go types it
// serves, with their doc comments, and checked in as agent.openapi.json (the
// same pattern as config.schema.json). This test regenerates it and fails when
// the checked-in file is stale:
//
//	make agent-openapi
var updateAgentOpenAPI = flag.Bool("update-agent-openapi", false, "rewrite serv/agent.openapi.json")

const agentOpenAPIFile = "agent.openapi.json"

func TestAgentOpenAPIIsCurrent(t *testing.T) {
	got := generateAgentOpenAPI(t)
	if *updateAgentOpenAPI {
		if err := os.WriteFile(agentOpenAPIFile, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if !bytes.Equal(got, agentOpenAPISpec) {
		t.Fatalf("%s is stale: the agent API types changed. Run `make agent-openapi` and commit the result.", agentOpenAPIFile)
	}
}

// agentRequestDoc is the POST /api/v1/agent body: the agent request plus the
// service-level thread_id.
type agentRequestDoc struct {
	gjagent.Request
	ThreadID string `json:"thread_id,omitempty" jsonschema:"description=Run the turn in this thread (create it with the gj_thread GraphQL root). Its stored history replaces any history in the body\\, and the turn is stored with its run."`
}

// Each schema name a reflected Go type gets in components.schemas.
var agentOpenAPITypeNames = map[reflect.Type]string{
	reflect.TypeOf(agentRequestDoc{}):           "AgentRequest",
	reflect.TypeOf(gjagent.Response{}):          "AgentResponse",
	reflect.TypeOf(threadAgentResponse{}):       "AgentThreadResponse",
	reflect.TypeOf(agentStatusResponse{}):       "AgentStatus",
	reflect.TypeOf(gjagent.ActionEvent{}):       "AgentActionEvent",
	reflect.TypeOf(experienceResponse{}):        "AgentExperience",
	reflect.TypeOf(experienceSummaryResponse{}): "AgentExperienceSummary",
	reflect.TypeOf(experiencePage{}):            "AgentExperiencePage",
}

func generateAgentOpenAPI(t *testing.T) []byte {
	t.Helper()
	r := &jsonschema.Reflector{
		RequiredFromJSONSchemaTags: true,
		Namer: func(rt reflect.Type) string {
			if name, ok := agentOpenAPITypeNames[rt]; ok {
				return name
			}
			// Prefix every other type so it cannot collide with the REST
			// schemas generated from the database (a "users" table, say).
			if rt.Name() == "" || strings.HasPrefix(rt.Name(), "Agent") {
				return rt.Name()
			}
			return "Agent" + rt.Name()
		},
	}
	// AddGoComments keys comments by path.Join(base, dir), so each module is
	// walked from its own directory: "../agent" would key them under
	// .../agent/agent and match nothing.
	for _, m := range []struct{ pkg, dir string }{
		{"github.com/dosco/graphjin/agent/v3", "../agent"},
		{"github.com/dosco/graphjin/serv/v3", "../serv"},
	} {
		t.Chdir(m.dir)
		if err := r.AddGoComments(m.pkg, "."); err != nil {
			t.Fatalf("comments %s: %v", m.dir, err)
		}
	}

	schemas := map[string]any{}
	for rt := range agentOpenAPITypeNames {
		reflected := r.ReflectFromType(rt)
		raw, err := json.Marshal(reflected)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		for name, def := range mapFromAny(doc["$defs"]) {
			schemas[name] = def
		}
	}
	// The reply contract's statuses (agent/reply.go).
	statuses := []any{gjagent.StatusAnswered, gjagent.StatusNeedsClarification, gjagent.StatusNeedsApproval, gjagent.StatusBlocked, gjagent.StatusError}
	for _, name := range []string{"AgentResponse", "AgentThreadResponse"} {
		if status := mapFromAny(mapFromAny(mapFromAny(schemas[name])["properties"])["status"]); status != nil {
			status["enum"] = statuses
		}
	}
	// The status endpoint echoes these config blocks as written, and zero
	// means "use the default" there; their minimum/maximum/enum tags check
	// config input, not what the endpoint returns.
	for _, name := range []string{"AgentSelectionConfig", "AgentRateLimitConfig"} {
		for _, field := range mapFromAny(mapFromAny(schemas[name])["properties"]) {
			for _, keyword := range []string{"minimum", "maximum", "enum"} {
				delete(mapFromAny(field), keyword)
			}
		}
	}
	for name, schema := range agentOpenAPIRowSchemas() {
		schemas[name] = schema
	}

	doc := map[string]any{
		"paths":      agentOpenAPIPaths(),
		"components": map[string]any{"schemas": schemas},
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "Kind suggests a form") {
		t.Fatal("agent doc comments did not reach the schemas")
	}
	out = bytes.ReplaceAll(out, []byte(`"#/$defs/`), []byte(`"#/components/schemas/`))
	return append(out, '\n')
}

func ref(name string) map[string]any { return map[string]any{"$ref": "#/components/schemas/" + name} }

func jsonContent(schema any) map[string]any {
	return map[string]any{"application/json": map[string]any{"schema": schema}}
}

func jsonResponse(description string, schema any) map[string]any {
	return map[string]any{"description": description, "content": jsonContent(schema)}
}

func objectOf(properties map[string]any, required ...string) map[string]any {
	out := map[string]any{"type": "object", "properties": properties}
	if len(required) != 0 {
		out["required"] = required
	}
	return out
}

func prop(typ, description string) map[string]any {
	return map[string]any{"type": typ, "description": description}
}

func arrayOf(items any) map[string]any { return map[string]any{"type": "array", "items": items} }

func errorBody() map[string]any {
	return objectOf(map[string]any{"error": prop("string", "What went wrong.")}, "error")
}

func pathParam(name, description string) map[string]any {
	return map[string]any{"name": name, "in": "path", "required": true, "description": description, "schema": map[string]any{"type": "string"}}
}

const agentSSEDescription = "With `Accept: text/event-stream` the turn streams as server-sent events: " +
	"`run` ({thread_id, run_id}, threaded turns only), `action` (AgentActionEvent, one per tool step), " +
	"`delta` (AgentDeltaEvent, the reply text as it is written; not stored, so a reconnecting client never sees it), " +
	"`result` (the final AgentResponse, authoritative), `complete` ({status}) and `error` ({error})."

// agentOpenAPIPaths describes every agent route. x-graphjin-feature names the
// switch a path needs; the served spec keeps only the paths that are on.
func agentOpenAPIPaths() map[string]any {
	agentTag := []string{"agent"}
	return map[string]any{
		routeAgent: map[string]any{
			"x-graphjin-feature": "agent",
			"post": map[string]any{
				"operationId": "askAgent",
				"tags":        agentTag,
				"summary":     "Ask the agent",
				"description": "Runs one agent turn. Without thread_id the call is stateless; with it the turn is stored in that thread and the reply carries thread_id and run_id. " + agentSSEDescription,
				"requestBody": map[string]any{"required": true, "content": jsonContent(ref("AgentRequest"))},
				"responses": map[string]any{
					"200": map[string]any{
						"description": "The agent's reply. Check status: answered, needs_clarification, needs_approval (approval holds the pending write), blocked (refusal says why) or error.",
						"content": map[string]any{
							"application/json":  map[string]any{"schema": map[string]any{"anyOf": []any{ref("AgentThreadResponse"), ref("AgentResponse")}}},
							"text/event-stream": map[string]any{"schema": prop("string", agentSSEDescription)},
						},
					},
					"400": jsonResponse("The body could not be read, or names a thread when threads are off.", ref("AgentResponse")),
					"404": jsonResponse("The agent is disabled.", ref("AgentResponse")),
				},
			},
		},
		routeAgentStatus: map[string]any{
			"x-graphjin-feature": "status",
			"get": map[string]any{
				"operationId": "getAgentStatus",
				"tags":        agentTag,
				"summary":     "Agent readiness and what the caller may do",
				"responses":   map[string]any{"200": jsonResponse("Readiness, model settings and the caller's capability profile.", ref("AgentStatus"))},
			},
		},
		routeAgentRuns + "{run_id}/events": map[string]any{
			"x-graphjin-feature": "threads",
			"get": map[string]any{
				"operationId": "streamAgentRunEvents",
				"tags":        agentTag,
				"summary":     "Follow a threaded run",
				"description": "Replays the run's stored events after Last-Event-ID (or ?after=N), then follows new ones until `complete`. Any replica can serve it. Events: `action`, `result`, `complete`, `error`; `delta` is live-only and never replayed.",
				"parameters": []any{
					pathParam("run_id", "The run id from the `run` event or a thread response."),
					map[string]any{"name": "after", "in": "query", "description": "Replay events after this sequence number.", "schema": map[string]any{"type": "integer"}},
					map[string]any{"name": "Last-Event-ID", "in": "header", "description": "Standard SSE resume; wins over ?after.", "schema": map[string]any{"type": "string"}},
				},
				"responses": map[string]any{
					"200": map[string]any{"description": "Server-sent events.", "content": map[string]any{"text/event-stream": map[string]any{"schema": map[string]any{"type": "string"}}}},
					"404": jsonResponse("No such run for this caller.", errorBody()),
				},
			},
		},
		routeAgentRuns + "{run_id}/cancel": map[string]any{
			"x-graphjin-feature": "threads",
			"post": map[string]any{
				"operationId": "cancelAgentRun",
				"tags":        agentTag,
				"summary":     "Cancel a threaded run on any replica",
				"parameters":  []any{pathParam("run_id", "The run to cancel.")},
				"responses": map[string]any{
					"202": jsonResponse("Cancel requested.", objectOf(map[string]any{"data": ref("AgentRun")}, "data")),
					"404": jsonResponse("No such run for this caller.", errorBody()),
				},
			},
		},
		routeAgentProfiles + "/{profile_id}/thread": map[string]any{
			"x-graphjin-feature": "profiles",
			"get": map[string]any{
				"operationId": "getAgentProfileThread",
				"tags":        agentTag,
				"summary":     "The caller's home thread with a saved agent",
				"description": "Finds or creates it. Profiles themselves are gj_artifacts rows of kind agent_profile.",
				"parameters":  []any{pathParam("profile_id", "The agent_profile artifact id.")},
				"responses": map[string]any{
					"200": jsonResponse("The home thread.", objectOf(map[string]any{"data": ref("AgentThread")}, "data")),
					"404": jsonResponse("No such profile visible to the caller.", errorBody()),
				},
			},
		},
		routeAgentApprovals: map[string]any{
			"x-graphjin-feature": "approvals",
			"get": map[string]any{
				"operationId": "listAgentApprovals",
				"tags":        agentTag,
				"summary":     "The caller's write approvals, newest first",
				"responses": map[string]any{
					"200": jsonResponse("Approvals.", objectOf(map[string]any{"approvals": arrayOf(ref("AgentApprovalRecord"))}, "approvals")),
				},
			},
		},
		routeAgentApprovals + "/{approval_id}": map[string]any{
			"x-graphjin-feature": "approvals",
			"parameters":         []any{pathParam("approval_id", "The approval id from a needs_approval reply.")},
			"get": map[string]any{
				"operationId": "getAgentApproval",
				"tags":        agentTag,
				"summary":     "One write approval",
				"responses": map[string]any{
					"200": jsonResponse("The approval.", ref("AgentApprovalRecord")),
					"404": jsonResponse("No such approval for this caller.", errorBody()),
				},
			},
			"post": map[string]any{
				"operationId": "decideAgentApproval",
				"tags":        agentTag,
				"summary":     "Approve or reject a held write",
				"description": "Approving runs exactly the stored query and variables, once, as the caller. The hash must match the one in the approval.",
				"requestBody": map[string]any{"required": true, "content": jsonContent(objectOf(map[string]any{
					"decision": map[string]any{"type": "string", "enum": []any{"approve", "reject"}},
					"hash":     prop("string", "The approval's hash, binding the decision to the exact write."),
				}, "decision", "hash"))},
				"responses": map[string]any{
					"200": jsonResponse("The decided approval; status is executed, failed or rejected.", ref("AgentApprovalRecord")),
					"404": jsonResponse("No such approval for this caller.", errorBody()),
					"409": jsonResponse("Already decided, expired, or the hash does not match.", errorBody()),
					"422": jsonResponse("The decision could not be applied.", errorBody()),
				},
			},
		},
		routeNotifications: map[string]any{
			"x-graphjin-feature": "notifications",
			"get": map[string]any{
				"operationId": "listNotifications",
				"tags":        []string{"notifications"},
				"summary":     "The caller's notifications, newest first",
				"description": "Changes waiting for approval, routine answers, stopped runs and task checks. Mark them seen here or through gj_notification(update: { seen }).",
				"parameters": []any{
					map[string]any{"name": "unseen", "in": "query", "description": "Only unseen ones.", "schema": map[string]any{"type": "string", "enum": []any{"1", "true"}}},
					map[string]any{"name": "limit", "in": "query", "description": "At most this many (default 50, max 200).", "schema": map[string]any{"type": "integer"}},
				},
				"responses": map[string]any{
					"200": jsonResponse("Notifications and the unseen count.", objectOf(map[string]any{
						"notifications": arrayOf(ref("AgentNotification")),
						"unseen_count":  prop("integer", "Unseen notifications in all."),
					}, "notifications", "unseen_count")),
					"401": jsonResponse("Notifications need a signed-in user.", errorBody()),
				},
			},
		},
		routeNotifications + "/seen": map[string]any{
			"x-graphjin-feature": "notifications",
			"post": map[string]any{
				"operationId": "markAllNotificationsSeen",
				"tags":        []string{"notifications"},
				"summary":     "Mark every unseen notification seen",
				"responses":   map[string]any{"200": jsonResponse("How many were marked.", objectOf(map[string]any{"marked": prop("integer", "Notifications marked seen.")}, "marked"))},
			},
		},
		routeNotifications + "/{notification_id}/seen": map[string]any{
			"x-graphjin-feature": "notifications",
			"post": map[string]any{
				"operationId": "markNotificationSeen",
				"tags":        []string{"notifications"},
				"summary":     "Mark one notification seen or unseen",
				"parameters":  []any{pathParam("notification_id", "The notification id.")},
				"requestBody": map[string]any{"content": jsonContent(objectOf(map[string]any{"seen": prop("boolean", "Default true; false marks it unseen.")}))},
				"responses": map[string]any{
					"200": jsonResponse("Marked.", objectOf(map[string]any{"id": prop("string", "The notification."), "seen": prop("boolean", "Its state now.")}, "id", "seen")),
					"404": jsonResponse("No such notification for this caller.", errorBody()),
				},
			},
		},
		routeNotifications + "/stream": map[string]any{
			"x-graphjin-feature": "notifications",
			"get": map[string]any{
				"operationId": "streamNotifications",
				"tags":        []string{"notifications"},
				"summary":     "Follow new notifications live",
				"description": "Server-sent events: `notification` (AgentNotification) for each one created after the cursor, and `count` ({unseen_count}) whenever the caller's unseen count changes. The event id is the notification's creation time in microseconds; resume with Last-Event-ID or ?after=. Without a cursor the stream starts now; list the backlog with GET /api/v1/notifications.",
				"parameters": []any{
					map[string]any{"name": "after", "in": "query", "description": "Resume after this event id.", "schema": map[string]any{"type": "integer"}},
					map[string]any{"name": "Last-Event-ID", "in": "header", "description": "Standard SSE resume; wins over ?after.", "schema": map[string]any{"type": "string"}},
				},
				"responses": map[string]any{"200": map[string]any{"description": "Server-sent events.", "content": map[string]any{"text/event-stream": map[string]any{"schema": map[string]any{"type": "string"}}}}},
			},
		},
		routeAgentReliability: map[string]any{
			"x-graphjin-feature": "approvals",
			"get": map[string]any{
				"operationId": "getAgentReliability",
				"tags":        agentTag,
				"summary":     "How the agent's changes turned out",
				"description": "The caller's changes whose outcome was checked (a change proposed with expect), tallied overall, per routine (watch), per saved agent and per kind of change.",
				"responses": map[string]any{
					"200": jsonResponse("The ledger.", objectOf(map[string]any{
						"overall":    ref("AgentReliabilityTally"),
						"by_routine": arrayOf(ref("AgentReliabilityTally")),
						"by_agent":   arrayOf(ref("AgentReliabilityTally")),
						"by_change":  arrayOf(ref("AgentReliabilityTally")),
					}, "overall", "by_routine", "by_agent", "by_change")),
					"401": jsonResponse("The ledger needs a signed-in user.", errorBody()),
				},
			},
		},
		routeAgentExperience: map[string]any{
			"x-graphjin-feature": "threads",
			"get": map[string]any{
				"operationId": "listAgentExperience",
				"tags":        agentTag,
				"summary":     "Past questions as tests",
				"description": "Admin only. Reads finished thread turns, newest first, and turns each question the lead asked the data agent into a test when the query that answered it computes one figure in the database. The query is re-run as the person who asked, and the answer they were given must pass it. confirmed tests had a thumbs-up on a one-question turn; incumbent tests were never rated and measure agreement, not correctness. Nothing is stored. Save the cases and run graphjin eval mine to turn them into a suite.",
				"parameters": []any{
					map[string]any{"name": "since", "in": "query", "description": "Only runs created at or after this RFC 3339 time.", "schema": map[string]any{"type": "string"}},
					map[string]any{"name": "until", "in": "query", "description": "Only runs created at or before this RFC 3339 time.", "schema": map[string]any{"type": "string"}},
					map[string]any{"name": "origin", "in": "query", "description": "Only turns of this kind.", "schema": map[string]any{"type": "string", "enum": []any{"chat", "routine", "continuation", "outcome"}}},
					map[string]any{"name": "limit", "in": "query", "description": "Runs to read (default 50, at most 200).", "schema": map[string]any{"type": "integer"}},
					map[string]any{"name": "offset", "in": "query", "description": "Runs to skip, from page.next_offset.", "schema": map[string]any{"type": "integer"}},
					map[string]any{"name": "format", "in": "query", "schema": map[string]any{"type": "string", "enum": []any{"summary"}}},
				},
				"responses": map[string]any{
					"200": jsonResponse("Mined tests, or the counts alone with ?format=summary.", map[string]any{"oneOf": []any{
						ref("AgentExperience"), ref("AgentExperienceSummary"),
					}}),
					"401": jsonResponse("Experience needs a signed-in admin.", errorBody()),
					"403": jsonResponse("The caller is not an admin.", errorBody()),
				},
			},
		},
		routeAgentFeedback: map[string]any{
			"x-graphjin-feature": "threads",
			"get": map[string]any{
				"operationId": "listAgentFeedback",
				"tags":        agentTag,
				"summary":     "Answer feedback",
				"description": "The caller's ratings of agent answers (an admin reads everyone's). Write a rating through gj_artifacts with kind feedback, target_ref the message id, content_json {rating: up|down} and content the correction. ?format=corpus returns wrong-answer reports as draft lead-corpus cases.",
				"parameters": []any{
					map[string]any{"name": "format", "in": "query", "schema": map[string]any{"type": "string", "enum": []any{"corpus"}}},
				},
				"responses": map[string]any{
					"200": jsonResponse("Reports, or corpus drafts with ?format=corpus.", map[string]any{"oneOf": []any{
						objectOf(map[string]any{"feedback": arrayOf(ref("AgentFeedbackReport"))}, "feedback"),
						objectOf(map[string]any{"cases": arrayOf(ref("AgentFeedbackCorpusCase"))}, "cases"),
					}}),
					"401": jsonResponse("Feedback needs a signed-in user.", errorBody()),
				},
			},
		},
	}
}

// agentOpenAPIRowSchemas describes the store rows the REST endpoints return as
// maps. Their property sets are checked against the row builders below.
func agentOpenAPIRowSchemas() map[string]any {
	str := func(description string) map[string]any { return prop("string", description) }
	return map[string]any{
		"AgentNotification": objectOf(map[string]any{
			"id":           str("Notification id."),
			"kind":         map[string]any{"type": "string", "description": "What happened.", "enum": []any{notificationApprovalPending, notificationRoutineAnswer, notificationRunFailed, notificationTaskVerifyFail, "task_verified", notificationAutonomousWrite, notificationOutcomeHit, notificationOutcomeMiss, refusalBudgetExceeded}},
			"severity":     map[string]any{"type": "string", "enum": []any{notificationSeverityInfo, notificationSeverityAction, notificationSeverityWarning}, "description": "action: something waits for you."},
			"title":        str("One line."),
			"body":         str("Detail, such as the change's summary or the routine's answer."),
			"ref_kind":     str("What it is about: approval, thread or task."),
			"ref_id":       str("Its id."),
			"link":         str("A console path that opens it."),
			"delegated_by": str("Who delegated the work, when an agent acted for them."),
			"seen":         prop("boolean", "Whether the owner has seen it."),
			"seen_at":      str("When it was marked seen."),
			"created_at":   str("UTC, fixed width: text order is time order."),
		}, "id", "kind", "title", "seen", "created_at"),
		"AgentDeltaEvent": objectOf(map[string]any{
			"text":  str("The reply text so far."),
			"reset": prop("boolean", "The text restarted (the model began a new draft); replace what was shown."),
		}, "text", "reset"),
		"AgentApprovalRecord": objectOf(map[string]any{
			"id":           str("Approval id."),
			"thread_id":    str("The thread of the turn that proposed it, if any."),
			"run_id":       str("The run that proposed it, if any."),
			"query":        str("The exact mutation that runs on approval."),
			"variables":    map[string]any{"description": "Its variables."},
			"namespace":    str("The namespace it runs in."),
			"summary":      map[string]any{"description": "What it changes: target, operation and values."},
			"preview":      map[string]any{"description": "Rows from a dry run that was rolled back, or {available: false, reason}."},
			"hash":         str("Binds a decision to this exact write."),
			"status":       map[string]any{"type": "string", "enum": []any{approvalStatusPending, approvalStatusExecuted, approvalStatusFailed, approvalStatusRejected, approvalStatusExpired}},
			"result":       map[string]any{"description": "The write's result once executed."},
			"error":        str("Why an approved write failed."),
			"created_at":   str("RFC 3339."),
			"expires_at":   str("RFC 3339; a pending approval reads as expired after it."),
			"decided_at":   str("RFC 3339."),
			"owner_id":     str("Who asked; only they can decide it."),
			"origin":       map[string]any{"type": "string", "enum": []any{"", approvalOriginChat, approvalOriginRoutine, approvalOriginContinuation}, "description": "What proposed it: a chat turn, a watch routine, or a continuation after an earlier decision."},
			"watch_id":     str("The watch whose routine proposed it."),
			"event_id":     str("The watch event that routine ran for."),
			"profile":      str("The saved agent the run used, if any."),
			"decided_by":   str("Who or what decided it: the owner, or autonomy:<rule> when a configured rule let it run."),
			"delegated_by": str("Whom a service-identity run acted for."),
			"expect":       map[string]any{"description": "What the change should achieve, checked after it runs: {query, path, op, value, within_hours, baseline, text}. null when none was given."},
			"outcome":      map[string]any{"description": "The outcome check once the change ran: {status: pending|checking|hit|miss|error, value, due_at, checked_at}. null before."},
		}, "id", "hash", "status", "query"),
		"AgentReliabilityTally": objectOf(map[string]any{
			"key":      str("What is tallied: all, a watch id, an agent_profile id, or a change such as data.update support_tickets."),
			"checked":  prop("integer", "Changes checked."),
			"hits":     prop("integer", "Changes that did what they were expected to."),
			"misses":   prop("integer", "Changes that did not."),
			"hit_rate": map[string]any{"type": []any{"number", "null"}, "description": "hits / checked."},
		}, "key", "checked", "hits", "misses"),
		"AgentThread": objectOf(map[string]any{
			"id":              str("Thread id."),
			"title":           str("Title."),
			"profile":         str("The saved agent (agent_profile id) the thread talks to, if any."),
			"status":          str("Thread status."),
			"channel":         str("Where the thread lives."),
			"active_run_id":   str("The running turn, if any."),
			"last_message_at": str("RFC 3339."),
			"last_seen_at":    str("RFC 3339."),
			"unread":          prop("boolean", "An answer arrived after the owner last looked."),
			"created_at":      str("RFC 3339."),
			"updated_at":      str("RFC 3339."),
			"owner_ref":       str("An opaque reference to the owner."),
		}, "id"),
		"AgentRun": objectOf(map[string]any{
			"id":          str("Run id."),
			"thread_id":   str("Its thread."),
			"status":      map[string]any{"type": "string", "enum": []any{runStatusRunning, runStatusCancelRequested, runStatusDone, runStatusCancelled, runStatusFailed}},
			"tokens_in":   prop("integer", "Model input tokens."),
			"tokens_out":  prop("integer", "Model output tokens."),
			"llm_calls":   prop("integer", "Model calls."),
			"error":       str("Why it failed."),
			"created_at":  str("RFC 3339."),
			"updated_at":  str("RFC 3339."),
			"finished_at": str("RFC 3339."),
		}, "id", "status"),
		"AgentFeedbackReport": objectOf(map[string]any{
			"id":            str("Feedback artifact id."),
			"message_id":    str("The rated answer."),
			"thread_id":     str("Its thread."),
			"run_id":        str("Its run."),
			"question":      str("The question it answered, from the thread."),
			"answer":        str("The answer as stored."),
			"answer_status": str("The answer's reply status."),
			"trace_id":      str("The run's trace id."),
			"history":       arrayOf(objectOf(map[string]any{"role": str("user or assistant"), "content": str("The turn.")})),
			"queries":       arrayOf(str("A query the data agent ran for this answer, in order.")),
			"rating":        map[string]any{"type": "string", "enum": []any{feedbackRatingUp, feedbackRatingDown}},
			"correction":    str("What the answer should have said, in the person's words."),
			"updated_at":    str("RFC 3339."),
		}, "id", "message_id", "rating"),
		"AgentFeedbackCorpusCase": objectOf(map[string]any{
			"id":       str("Draft case id."),
			"group":    str("Always field."),
			"prompt":   str("The question."),
			"turns":    arrayOf(str("An earlier user turn to replay.")),
			"draft":    prop("boolean", "Always true: a draft has no oracle yet."),
			"observed": objectOf(map[string]any{"answer": str("What the agent said."), "status": str("Its reply status."), "trace_id": str("Its trace."), "queries": arrayOf(str("A query the data agent ran, in order: where an oracle starts."))}),
			"expected": str("The person's correction."),
			"todo":     str("What to add before the case joins the corpus."),
		}, "id", "prompt", "draft"),
	}
}

// The hand-written row schemas must name exactly the keys the row builders
// return, or the spec would document fields the server never sends.
func TestAgentOpenAPIRowSchemasMatchTheRows(t *testing.T) {
	schemas := agentOpenAPIRowSchemas()
	for name, row := range map[string]map[string]any{
		"AgentApprovalRecord": approvalAPIRow(map[string]any{}, time.Now()),
		"AgentThread":         threadAPIRow(map[string]any{}),
		"AgentRun":            runAPIRow(map[string]any{}),
		"AgentNotification":   notificationAPIRow(map[string]any{}),
	} {
		if got, want := schemaKeys(schemas[name]), mapKeys(row); !reflect.DeepEqual(got, want) {
			t.Errorf("%s properties = %v, row keys = %v", name, got, want)
		}
	}

	svc, threadID := newFeedbackService(t)
	ctx := approvalUserCtx("user_1")
	if _, err := rateAnswer(svc, ctx, threadID+"-00000004", "down", "It is $1,850."); err != nil {
		t.Fatal(err)
	}
	reports := mustFeedback(t, svc, ctx)
	if got, want := schemaKeys(schemas["AgentFeedbackReport"]), mapKeys(reports[0]); !reflect.DeepEqual(got, want) {
		t.Errorf("AgentFeedbackReport properties = %v, report keys = %v", got, want)
	}
	drafts := feedbackCorpusDrafts(reports)
	if got, want := schemaKeys(schemas["AgentFeedbackCorpusCase"]), mapKeys(drafts[0]); !reflect.DeepEqual(got, want) {
		t.Errorf("AgentFeedbackCorpusCase properties = %v, draft keys = %v", got, want)
	}
}

// Every agent route the server registers is described.
func TestAgentOpenAPICoversEveryAgentRoute(t *testing.T) {
	var doc struct {
		Paths map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(agentOpenAPISpec, &doc); err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{routeAgent, routeAgentStatus, routeAgentRuns, routeAgentProfiles, routeAgentApprovals, routeAgentFeedback, routeAgentExperience, routeNotifications, routeAgentReliability} {
		found := false
		for path := range doc.Paths {
			if path == route || strings.HasPrefix(path, strings.TrimSuffix(route, "/")+"/") {
				found = true
			}
		}
		if !found {
			t.Errorf("route %s has no path in %s", route, agentOpenAPIFile)
		}
	}
}

func schemaKeys(schema any) []string {
	return mapKeys(mapFromAny(mapFromAny(schema)["properties"]))
}

func mapKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Real serialized values must validate against the schemas the spec serves:
// a field the server sends but the spec does not describe fails here.
func TestAgentOpenAPISchemasAcceptWhatTheServerSends(t *testing.T) {
	rest := []byte(`{"openapi":"3.0.0","info":{"title":"GraphJin REST API","version":"1.0.0"},"paths":{"/top_accounts":{"get":{"responses":{"200":{"description":"ok"}}}}},"components":{"schemas":{"accounts":{"type":"object"}}}}`)
	all := map[string]bool{"status": true, "agent": true, "threads": true, "profiles": true, "approvals": true, "notifications": true}
	merged, err := withAgentOpenAPI(rest, all)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := schemavalidator.UnmarshalJSON(bytes.NewReader(merged))
	if err != nil {
		t.Fatal(err)
	}
	c := schemavalidator.NewCompiler()
	if err := c.AddResource("spec.json", doc); err != nil {
		t.Fatal(err)
	}
	validate := func(schema string, value any) {
		t.Helper()
		s, err := c.Compile("spec.json#/components/schemas/" + schema)
		if err != nil {
			t.Fatalf("compile %s: %v", schema, err)
		}
		raw, _ := json.Marshal(value)
		inst, err := schemavalidator.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Validate(inst); err != nil {
			t.Errorf("%s rejects %s: %v", schema, raw, err)
		}
	}

	resp := gjagent.Response{
		Status: gjagent.StatusNeedsApproval, Answer: "This change needs your approval.",
		Figures:  []gjagent.Figure{{Text: "$300.00", Source: gjagent.FigureSourceDataAgent}, {Text: "12"}},
		Data:     map[string]any{"invoices": []any{map[string]any{"amount_cents": 30000}}},
		Approval: &gjagent.Approval{ID: "apr_1", Hash: "h", Summary: "update 1 row", Query: "mutation { x }", Variables: map[string]any{"id": 1}, ExpiresAt: "2026-11-10T00:00:00Z"},
		Display: []gjagent.DisplayHint{{Root: "invoices", Kind: "line", Question: "q", RowCount: 1, Rows: []any{map[string]any{"due_at": "2026-11-04"}},
			Columns: []gjagent.DisplayColumn{{Name: "due_at", Type: "text", Role: "time"}, {Name: "sum_amount_cents", Role: "measure", Aggregate: "sum"}}}},
		Notices:  []gjagent.ResponseNotice{{Kind: "unverified_figures", Message: "m", Count: 1, WatchIDs: []string{"w"}}},
		Errors:   []gjagent.ErrorInfo{{Message: "e", Extensions: map[string]any{"code": "x"}}},
		Refusal:  &gjagent.Refusal{Code: "c", Because: []string{"b"}, Unblock: []gjagent.UnblockStep{{}}, PolicyFinal: true},
		Usage:    map[string]any{"tokens": 1},
		TraceID:  "agent-1",
		Skills:   []gjagent.SkillUsage{{}},
		Memories: []gjagent.MemoryUsage{{}},
	}
	validate("AgentResponse", resp)
	validate("AgentThreadResponse", threadAgentResponse{Response: resp, ThreadID: "th_1", RunID: "run_1"})
	validate("AgentRequest", map[string]any{"instruction": "hi", "thread_id": "th_1", "history": []any{map[string]any{"role": "user", "content": "x"}}, "max_steps": 4, "return_trace": true})
	validate("AgentStatus", agentStatusResponse{Status: "ready", Enabled: true, Selection: &gjagent.SelectionConfig{}})
	validate("AgentActionEvent", gjagent.ActionEvent{Index: 1, Tool: "execute_graphql", Args: map[string]any{"query": "{}"}, Status: "ok", Delegate: 1})
	// Store rows hold strings: every approval column is NOT NULL DEFAULT ''.
	approvalRow := map[string]any{"variables_json": `{"id":1}`, "summary_json": `{"table":"t"}`, "preview_json": `{"available":false}`, "result_json": ""}
	for _, column := range strings.Fields(approvalStoreFields) {
		if _, ok := approvalRow[column]; !ok {
			approvalRow[column] = ""
		}
	}
	approvalRow["status"] = approvalStatusPending
	validate("AgentApprovalRecord", approvalAPIRow(approvalRow, time.Now()))
	validate("AgentDeltaEvent", map[string]any{"text": "Here", "reset": false})
	validate("AgentNotification", notificationAPIRow(map[string]any{"id": "ntf_1", "kind": notificationApprovalPending, "severity": notificationSeverityAction, "title": "t", "seen": false, "created_at": notificationTime(time.Now())}))

	// The served document keeps core's REST paths and only the agent paths
	// whose feature is on.
	var out struct {
		OpenAPI string         `json:"openapi"`
		Paths   map[string]any `json:"paths"`
	}
	partial, err := withAgentOpenAPI(rest, map[string]bool{"status": true, "agent": true})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(partial, &out); err != nil {
		t.Fatal(err)
	}
	if out.OpenAPI != "3.1.0" || out.Paths["/top_accounts"] == nil || out.Paths[routeAgent] == nil || out.Paths[routeAgentApprovals] != nil || out.Paths[routeAgentFeedback] != nil {
		t.Fatalf("openapi %s, paths %v", out.OpenAPI, mapKeys(out.Paths))
	}
	if strings.Contains(string(partial), "x-graphjin-feature") {
		t.Fatal("feature markers leaked into the served spec")
	}
}
