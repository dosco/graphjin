package eval

import (
	"crypto/sha256"
	"encoding/hex"
)

// The prompts a model authors tasks from.
//
// They are written in the same signature form the agent's own single-call
// programs use. Each asks for a JSON array so one call yields several picks, and
// each says plainly what the engine will reject — a model told the rules up
// front wastes fewer calls being refused.
//
// These are versioned by hash rather than by number: a task records which
// prompts produced it, so a batch that turns out badly can be traced to the
// wording that caused it.

const watchAuthoringSignature = `"You are choosing which standing questions a company would want answered without asking. You are given a schema census: tables, and for some columns the closed set of values they hold. Pick rows worth watching — a state that means something has gone wrong or needs attention, like a failed payment or an urgent ticket — and never a routine state that changes constantly. For each pick give: table, column and value from the census (or omit column and value to watch the whole table), a short lowercase snake_case watch_name, and an intent: one or two sentences in the voice of the person who wants it, describing the standing need and that they want to be told rather than having to look. The intent must read like a colleague speaking and must never mention GraphJin, watches, subscriptions, queries, or any table or column name. Reply with only a JSON array."
census:string "The tables, closed value sets, and relationships available.",
count:string "How many picks to return."
-> picks_json:string "JSON array of {table, column, value, watch_name, intent}."`

const confirmationAuthoringSignature = `"You are writing the two turns before someone says yes. You are given a schema census. Pick rows worth alerting on, and for each write: need — one or two sentences where a colleague describes a problem they keep having, in their own voice, naming no table or column; and proposal — one or two sentences where an assistant replies offering to set up a specific named alert with an hourly digest, which may name the alert and the cadence. The user will then reply only 'Yes, go ahead and set that up.', so the proposal must be specific enough that agreeing to it is unambiguous. Reply with only a JSON array."
census:string "The tables, closed value sets, and relationships available.",
count:string "How many picks to return."
-> picks_json:string "JSON array of {table, column, value, need, proposal}."`

const historyAuthoringSignature = `"You are turning standalone questions into follow-ups that only make sense in context. You are given questions that have already been verified against the database, each with an id. For each pick: choose a task_id, write first_question (what someone asked just before, establishing a subject), prior_answer (the assistant's brief reply, which may state a figure), and follow_up — the question to be answered now, which must refer to the subject only by pronoun such as 'it', 'that account' or 'those', and must still be answered by exactly the same underlying question as the original. Reply with only a JSON array."
tasks:string "Verified questions with their ids.",
count:string "How many picks to return."
-> picks_json:string "JSON array of {task_id, first_question, prior_answer, follow_up}."`

const scenarioAuthoringSignature = `"You are restating questions as the situations that would prompt them. You are given questions already verified against the database, each with an id. For each pick, rewrite the question as one or two sentences describing a real moment at work that leads to exactly the same question — a meeting starting, a report due, a customer complaining. The rewrite must ask for exactly the same thing, must not change what is being measured, and must not contain any table or column name from the original. Reply with only a JSON array."
tasks:string "Verified questions with their ids.",
count:string "How many picks to return."
-> picks_json:string "JSON array of {task_id, prompt}."`

const fileAuthoringSignature = `"You are choosing rules a company writes down in a document rather than storing in its database. You are given a schema census, including one or more file sources that hold written policy documents. For each pick choose: file_root, the name of a file source from the census; table, a table from the census; optionally column and value from the census closed sets to narrow which rows matter; policy_topic, a short title for the rule such as 'incident response times'; policy_answer, the rule itself as one short phrase under forty characters, specific and checkable, such as '4 hours' or 'two business days'; intent, one or two sentences in the voice of someone who needs both the current numbers and the standard they are held to, which must NOT mention the file source, documents, or files at all, because working out that the standard is written down somewhere is the point; and execution, one or two sentences asking plainly for both the written standard and the current count. Neither sentence may name GraphJin, queries or tools. Reply with only a JSON array."
census:string "The tables, closed value sets, relationships and file sources available.",
count:string "How many picks to return."
-> picks_json:string "JSON array of {file_root, table, column, value, policy_topic, policy_answer, intent, execution}."`

// AuthoringPromptsHash identifies the wording a batch of tasks was authored
// under, so provenance points at the prompts and not only at the model. A batch
// that turns out badly can then be traced to what was asked for.
func AuthoringPromptsHash() string {
	sum := sha256.Sum256([]byte(
		watchAuthoringSignature + confirmationAuthoringSignature +
			historyAuthoringSignature + scenarioAuthoringSignature +
			fileAuthoringSignature))
	return hex.EncodeToString(sum[:])[:8]
}
