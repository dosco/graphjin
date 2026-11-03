package serv

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

// threadRun is one agent turn on a persistent thread. prepareThreadRun claims
// the thread and records the user message; execute runs the agent detached
// from the caller's connection, so a client that disconnects can resume the
// stream from any replica and the answer still lands in the thread.
type threadRun struct {
	s        *graphjinService
	thread   map[string]any
	threadID string
	runID    string

	mu  sync.Mutex
	seq int64
}

// threadRunEmit receives each streamed run event after it is persisted.
type threadRunEmit func(seq int64, kind string, payload any)

// threadRunOptions are for runs a person did not type. RunID, when set, is the
// run's fixed id, so a second attempt at the same work fails on the insert
// instead of running the agent again; Display replaces the instruction as the
// user message the thread shows.
type threadRunOptions struct {
	RunID   string
	Display string
}

func (s *graphjinService) prepareThreadRun(ctx context.Context, threadID string, req *gjagent.Request) (*threadRun, error) {
	return s.prepareThreadRunWith(ctx, threadID, req, threadRunOptions{})
}

func (s *graphjinService) prepareThreadRunWith(ctx context.Context, threadID string, req *gjagent.Request, opts threadRunOptions) (*threadRun, error) {
	thread, err := s.ownedThreadStoreRow(ctx, threadID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Instruction) == "" {
		return nil, errors.New("instruction is required")
	}
	// The thread is the authoritative history; a caller-supplied history
	// would let two clients of one thread disagree about what was said.
	history, err := s.threadHistory(ctx, threadID, s.conf.Core.EffectiveThreadsConfig().HistoryTurns)
	if err != nil {
		return nil, err
	}
	req.History = history
	req.Profile = s.requestProfile(ctx, thread)

	runID := opts.RunID
	if runID == "" {
		runID = "run_" + taskRandomHex()
	}
	run := &threadRun{s: s, thread: thread, threadID: threadID, runID: runID}
	if err := s.claimThreadRun(ctx, threadID, run.runID); err != nil {
		return nil, err
	}
	release := func() { _ = s.releaseThreadRun(context.WithoutCancel(ctx), threadID, run.runID) }
	if err := s.insertRun(ctx, thread, run.runID); err != nil {
		release()
		return nil, err
	}
	shown := req.Instruction
	if opts.Display != "" {
		shown = opts.Display
	}
	if _, err := s.appendMessage(ctx, messageSpec{
		ThreadID: threadID, Role: "user", Content: shown, RunID: run.runID,
		AccountID: stringMapValue(thread, "account_id"), OwnerID: stringMapValue(thread, "owner_id"),
	}); err != nil {
		_ = s.finishRun(context.WithoutCancel(ctx), run.runID, runStatusFailed, err.Error(), gjagent.UsageTotals{})
		release()
		return nil, err
	}
	if stringMapValue(thread, "title") == "" {
		_, _ = s.internalStoreMutationRows(ctx, "threads", `where: { id: { eq: $id } }, update: $input`, "id",
			map[string]any{"id": threadID, "input": map[string]any{"title": threadTitleFrom(shown)}})
	}
	return run, nil
}

func threadTitleFrom(instruction string) string {
	title := strings.Join(strings.Fields(instruction), " ")
	if len(title) > 80 {
		title = truncateUTF8(title, 80) + "…"
	}
	return title
}

func (r *threadRun) nextSeq() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	return r.seq
}

func (r *threadRun) record(ctx context.Context, emit threadRunEmit, kind string, payload any) {
	// Stored events are replayed to any client of the run, so they are
	// sanitized before they are persisted, not just before they are streamed.
	payload = gjagent.SanitizeValue(payload, configuredAgentSecret(agentConfigFromService(r.s.conf)))
	seq := r.nextSeq()
	if err := r.s.appendRunEvent(ctx, r.runID, seq, kind, payload); err != nil && r.s.log != nil {
		r.s.log.Warnf("run %s: persist %s event: %v", r.runID, kind, err)
	}
	if emit != nil {
		emit(seq, kind, payload)
	}
}

// execute runs the agent and settles the run. settle turns the raw runner
// result into the caller-facing response (notices, sanitizing) exactly as the
// stateless endpoint does.
func (r *threadRun) execute(ctx context.Context, runner graphjinAgentRunner, req gjagent.Request,
	settle func(context.Context, gjagent.Response, error) (gjagent.Response, error), emit threadRunEmit,
) (gjagent.Response, error) {
	s := r.s
	base := context.WithoutCancel(ctx)
	runCtx, cancel := context.WithCancel(base)
	defer cancel()
	localRuns.Store(r.runID, cancel)
	defer localRuns.Delete(r.runID)

	// stopReason is written by the monitor goroutine and read only after
	// monitorDone closes.
	stopReason := ""
	stopMonitor := make(chan struct{})
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		r.monitor(base, stopMonitor, func(reason string) {
			stopReason = reason
			cancel()
		})
	}()

	callerObserver := req.Observer
	req.Observer = func(event gjagent.ActionEvent) {
		r.record(base, emit, "action", event)
		if callerObserver != nil {
			callerObserver(event)
		}
	}
	resp, err := runner.Run(runCtx, req)
	close(stopMonitor)
	<-monitorDone

	// A cancel through this replica cancels runCtx without the monitor seeing
	// it, so read the stored status once more before deciding.
	if stopReason == "" && runCtx.Err() != nil {
		if row, _ := s.runStoreRow(base, r.runID); row != nil && stringMapValue(row, "status") == runStatusCancelRequested {
			stopReason = runStatusCancelRequested
		}
	}
	if stopReason == runStatusFailed {
		// The sweep already failed this run and told the thread; writing an
		// answer now would contradict it.
		r.record(base, emit, "complete", map[string]any{"status": runStatusFailed})
		return gjagent.Response{Status: runStatusFailed}, errors.New("run lost its lease")
	}
	status := runStatusDone
	errMsg := ""
	switch {
	case stopReason == runStatusCancelRequested:
		status = runStatusCancelled
		resp = gjagent.Response{Status: runStatusCancelled, Answer: "Stopped."}
		err = nil
	case err != nil:
		status = runStatusFailed
		errMsg = err.Error()
	}
	if status != runStatusCancelled {
		resp, err = settle(base, resp, err)
		if err != nil {
			errMsg = err.Error()
		}
	}

	content := resp.Answer
	if content == "" && len(resp.Errors) > 0 {
		content = resp.Errors[0].Message
	}
	messageStatus := resp.Status
	if status != runStatusDone {
		messageStatus = status
	}
	if _, merr := s.appendMessage(base, messageSpec{
		ThreadID: r.threadID, Role: "assistant", Content: content, Status: messageStatus, RunID: r.runID,
		CatalogIDs: responseCatalogIDs(resp), ResponseJSON: s.storedResponseJSON(resp),
		AccountID: stringMapValue(r.thread, "account_id"), OwnerID: stringMapValue(r.thread, "owner_id"),
	}); merr != nil && s.log != nil {
		s.log.Warnf("run %s: persist answer: %v", r.runID, merr)
	}
	r.record(base, emit, "result", resp)
	r.record(base, emit, "complete", map[string]any{"status": status})
	if ferr := s.finishRun(base, r.runID, status, errMsg, gjagent.SummarizeUsage(resp.Usage)); ferr != nil && s.log != nil {
		s.log.Warnf("run %s: finish: %v", r.runID, ferr)
	}
	if rerr := s.releaseThreadRun(base, r.threadID, r.runID); rerr != nil && s.log != nil {
		s.log.Warnf("run %s: release thread: %v", r.runID, rerr)
	}
	return resp, err
}

// monitor renews the run lease and watches for a cancel requested on any
// replica until stop closes.
func (r *threadRun) monitor(ctx context.Context, stop <-chan struct{}, onStop func(reason string)) {
	lease := time.Duration(r.s.conf.Core.EffectiveThreadsConfig().RunLeaseSeconds) * time.Second
	renewEvery := lease / 3
	if renewEvery < runCancelPollInterval {
		renewEvery = runCancelPollInterval
	}
	ticker := time.NewTicker(runCancelPollInterval)
	defer ticker.Stop()
	lastRenew := time.Now()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		var status string
		if time.Since(lastRenew) >= renewEvery {
			status, _ = r.s.heartbeatRun(ctx, r.runID)
			lastRenew = time.Now()
		} else if row, err := r.s.runStoreRow(ctx, r.runID); err == nil && row != nil {
			status = stringMapValue(row, "status")
		}
		if status == runStatusCancelRequested {
			onStop(runStatusCancelRequested)
			return
		}
		if runIsTerminal(status) {
			// Another replica failed this run (lost lease): stop working on it.
			onStop(runStatusFailed)
			return
		}
	}
}

// responseCatalogIDs extracts the catalog ids a run inspected, the warm-start
// hints carried into the next turn's history.
func responseCatalogIDs(resp gjagent.Response) []string {
	if resp.Evidence == nil {
		return nil
	}
	raw, err := json.Marshal(resp.Evidence)
	if err != nil {
		return nil
	}
	var evidence struct {
		CatalogDetailIDs []string `json:"catalog_detail_ids"`
		Protocol         struct {
			CatalogDetailIDs []string `json:"catalog_detail_ids"`
		} `json:"protocol"`
	}
	if json.Unmarshal(raw, &evidence) != nil {
		return nil
	}
	ids := evidence.Protocol.CatalogDetailIDs
	if len(ids) == 0 {
		ids = evidence.CatalogDetailIDs
	}
	if len(ids) > 16 {
		ids = ids[:16]
	}
	return ids
}

// storedResponseJSON keeps the full response with the message so the console
// can re-render it; oversized responses drop their data and trace.
func (s *graphjinService) storedResponseJSON(resp gjagent.Response) string {
	limit := s.conf.Core.EffectiveThreadsConfig().ResponseMaxBytes
	raw, err := json.Marshal(resp)
	if err != nil {
		return ""
	}
	if len(raw) <= limit {
		return string(raw)
	}
	resp.Data, resp.Trace = nil, nil
	raw, err = json.Marshal(resp)
	if err != nil || len(raw) > limit {
		return ""
	}
	return string(raw)
}
