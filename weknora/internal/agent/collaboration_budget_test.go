package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/collaboration"
	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func budgetWaitResponse() mockResponse {
	return mockResponse{chunks: []types.StreamResponse{{
		ResponseType: types.ResponseTypeAnswer, Done: true, FinishReason: "tool_calls",
		ToolCalls: []types.LLMToolCall{{ID: "wait", Type: "function",
			Function: types.FunctionCall{Name: "wait_agent", Arguments: `{"timeout_ms":1000}`}}},
	}}}
}

func publishBudgetResult(t *testing.T, r *collaboration.Runtime, b *collaboration.Binding, name, status, answer string) {
	t.Helper()
	require.NoError(t, r.Store.Change(b.Context(t.Context()), b.Scope, func(g *collaboration.Group) error {
		for _, member := range g.Members {
			if member.Name != name {
				continue
			}
			run := member.Runs[len(member.Runs)-1]
			run.State = status
			run.Result = collaboration.Result{Answer: answer}
			if status == "failed" {
				run.Result.Error = "synthetic child failure"
			}
			payload, err := json.Marshal(map[string]any{"result": run.Result, "session_id": member.SessionID})
			if err != nil {
				return err
			}
			g.NextSequence++
			g.Events = append(g.Events, collaboration.Event{
				Sequence: g.NextSequence, From: name, RunID: run.ID, Kind: status, Content: string(payload),
			})
		}
		return nil
	}))
}

// Reproduces the production path: a wait consumes the last parent round while
// delegated work exists. Evidence must survive, and a partial answer must not
// imply that cancelled or failed child work completed successfully.
func TestCollaborationBudgetSummarizesEvidenceAndStopsRemainingWork(t *testing.T) {
	r, initial := engineCollaboration(t)
	b, err := r.AcquireParent(t.Context(), initial.Scope)
	require.NoError(t, err)
	for _, name := range []string{"done", "failed", "pending"} {
		_, err := r.Spawn(b.Context(t.Context()), b.Scope, name, name, "template", "{}", "synthetic task")
		require.NoError(t, err)
	}
	publishBudgetResult(t, r, b, "done", "succeeded", "verified chip evidence")
	publishBudgetResult(t, r, b, "failed", "failed", "incomplete failed research")
	b.WaitUntil = time.Now().Add(-time.Second)
	model := &finalSynthesisChat{mockChat: mockChat{responses: []mockResponse{budgetWaitResponse()}}}
	model.synthesize = func(ctx context.Context, messages []chat.Message, opts *chat.ChatOptions) (<-chan types.StreamResponse, error) {
		require.NoError(t, ctx.Err(), "ending collaboration must not cancel the bounded synthesis")
		require.Equal(t, "none", opts.ToolChoice)
		require.Empty(t, opts.Tools)
		require.Contains(t, opts.RuntimeNotice, "partial")
		g, err := r.Store.Read(ctx, b.Scope)
		require.NoError(t, err)
		require.Equal(t, "cancelled", g.Status, "new delegated work is barred before synthesis")
		require.Equal(t, "succeeded", g.Members[0].Runs[0].State)
		require.Equal(t, "failed", g.Members[1].Runs[0].State)
		require.Equal(t, "cancelled", g.Members[2].Runs[0].State)
		_, err = r.Spawn(ctx, b.Scope, "again", "again", "template", "{}", "must not run")
		require.ErrorIs(t, err, collaboration.ErrClosed)
		var input strings.Builder
		for _, message := range messages {
			input.WriteString(message.Content)
		}
		require.Contains(t, input.String(), "verified chip evidence")
		require.Contains(t, input.String(), "synthetic child failure")
		require.Contains(t, input.String(), "pending")
		require.Contains(t, input.String(), "cancelled")
		return finalSynthesisChunks(types.StreamResponse{ResponseType: types.ResponseTypeAnswer,
			Content: "已获取芯片资料；其余调研尚未完成。", Done: true, FinishReason: "stop"}), nil
	}
	engine := newTestEngine(t, model, withMaxIterations(1))
	engine.SetCollaboration(b)
	engine.toolRegistry = agenttools.NewToolRegistry()
	for _, tool := range agenttools.NewCollaborationTools(b, nil) {
		engine.toolRegistry.RegisterTool(tool)
	}
	state := &types.AgentState{}
	_, err = engine.executeLoop(t.Context(), state, "synthetic task", nil, nil, "s", "m")
	require.NoError(t, err)
	require.Equal(t, 1, state.CurrentRound, "waiting still has a bounded iteration cost")
	require.Len(t, model.mockChat.calls, 1)
	require.Len(t, model.finals, 1)
	require.True(t, state.IsComplete, "the run has ended, not every requested task")
	require.Contains(t, state.FinalAnswer, "阶段性结果")
	require.Contains(t, state.FinalAnswer, "未完成")
}

func TestCollaborationBudgetIncludesResultsArrivingOnLastNaturalStop(t *testing.T) {
	r, b := engineCollaboration(t)
	_, err := r.Spawn(t.Context(), b.Scope, "spawn", "research", "template", "{}", "task")
	require.NoError(t, err)
	model := &mockChat{responses: []mockResponse{
		{chunks: []types.StreamResponse{
			{ResponseType: types.ResponseTypeThinking, Content: "publish late result"},
			{ResponseType: types.ResponseTypeAnswer, Content: "premature answer", Done: true, FinishReason: "stop"},
		}},
		{chunks: []types.StreamResponse{{ResponseType: types.ResponseTypeAnswer, Content: "reviewed late result", Done: true, FinishReason: "stop"}}},
	}}
	engine := newTestEngine(t, model, withMaxIterations(1))
	engine.SetCollaboration(b)
	engine.eventBus.On(event.EventAgentThought, func(_ context.Context, evt event.Event) error {
		if data, ok := evt.Data.(event.AgentThoughtData); ok && data.Content == "publish late result" {
			publishBudgetResult(t, r, b, "research", "succeeded", "late durable evidence")
		}
		return nil
	})
	state := &types.AgentState{}
	_, err = engine.executeLoop(t.Context(), state, "task", nil, nil, "s", "m")
	require.NoError(t, err)
	require.Len(t, model.calls, 2)
	require.Equal(t, "none", model.opts[1].ToolChoice)
	var input strings.Builder
	for _, message := range model.calls[1] {
		input.WriteString(message.Content)
	}
	require.Equal(t, 1, strings.Count(input.String(), "Payload:\nlate durable evidence"), "late delivery is not lost or duplicated")
	require.Contains(t, state.FinalAnswer, "reviewed late result")
	require.Contains(t, state.FinalAnswer, "阶段性结果")
}

func TestCollaborationBudgetSynthesisFailurePropagates(t *testing.T) {
	r, b := engineCollaboration(t)
	_, err := r.Spawn(t.Context(), b.Scope, "spawn", "research", "template", "{}", "task")
	require.NoError(t, err)
	want := errors.New("synthetic final model failure")
	model := &finalSynthesisChat{synthesize: func(context.Context, []chat.Message, *chat.ChatOptions) (<-chan types.StreamResponse, error) {
		return nil, want
	}}
	engine := newTestEngine(t, model, withMaxIterations(1))
	engine.SetCollaboration(b)
	state := &types.AgentState{CurrentRound: 1}
	_, err = engine.executeLoop(t.Context(), state, "task", nil, nil, "s", "m")
	require.ErrorIs(t, err, want)
	require.Len(t, model.finals, 1)
	require.False(t, state.IsComplete)
	require.Empty(t, state.FinalAnswer)
	g, err := r.Store.Read(t.Context(), b.Scope)
	require.NoError(t, err)
	require.Equal(t, "cancelled", g.Status)
}

func TestCollaborationBudgetDoesNotSummarizeAfterUserCancellation(t *testing.T) {
	r, b := engineCollaboration(t)
	_, err := r.Spawn(t.Context(), b.Scope, "spawn", "research", "template", "{}", "task")
	require.NoError(t, err)
	model := &mockChat{}
	engine := newTestEngine(t, model, withMaxIterations(1))
	engine.SetCollaboration(b)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	state := &types.AgentState{CurrentRound: 1}
	_, _ = engine.executeLoop(ctx, state, "task", nil, nil, "s", "m")
	require.Empty(t, model.calls)
	require.Empty(t, state.FinalAnswer)
}

func TestCollaborationBudgetUserCancellationStopsSynthesis(t *testing.T) {
	r, b := engineCollaboration(t)
	_, err := r.Spawn(t.Context(), b.Scope, "spawn", "research", "template", "{}", "task")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	model := &finalSynthesisChat{synthesize: func(ctx context.Context, _ []chat.Message, _ *chat.ChatOptions) (<-chan types.StreamResponse, error) {
		cancel()
		return nil, ctx.Err()
	}}
	engine := newTestEngine(t, model, withMaxIterations(1))
	engine.SetCollaboration(b)
	state := &types.AgentState{CurrentRound: 1}
	_, err = engine.executeLoop(ctx, state, "task", nil, nil, "s", "m")
	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, model.finals, 1)
	require.False(t, state.IsComplete)
	require.Empty(t, state.FinalAnswer)
}

func TestCollaborationBudgetNaturalStopDoesNotWaitBeyondBudget(t *testing.T) {
	r, b := engineCollaboration(t)
	_, err := r.Spawn(t.Context(), b.Scope, "spawn", "research", "template", "{}", "task")
	require.NoError(t, err)
	model := &mockChat{responses: []mockResponse{
		{chunks: []types.StreamResponse{{ResponseType: types.ResponseTypeAnswer, Content: "optimistic draft", Done: true, FinishReason: "stop"}}},
		{chunks: []types.StreamResponse{{ResponseType: types.ResponseTypeAnswer, Content: "No research result was available.", Done: true, FinishReason: "stop"}}},
	}}
	engine := newTestEngine(t, model, withMaxIterations(1))
	engine.SetCollaboration(b)
	engine.EnableSuspension(json.RawMessage(`{}`))
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	state := &types.AgentState{}
	_, err = engine.executeLoop(ctx, state, "task", nil, nil, "s", "m")
	require.NoError(t, err)
	require.Len(t, model.calls, 2)
	require.Nil(t, state.Suspended)
	require.Contains(t, state.FinalAnswer, "No research result was available.")
	g, err := r.Store.Read(t.Context(), b.Scope)
	require.NoError(t, err)
	require.Nil(t, g.ParentWait)
	require.Equal(t, "cancelled", g.Status)
}

// Automatic recovery wraps Resume in its own lease context; Resume and the
// loop must reuse that lifetime. Closing child work must not let an outer
// heartbeat cancel a slow, tool-free report, while actual user cancellation
// must still stop it promptly.
func TestCollaborationBudgetResumeWithOuterLeaseAndSlowSynthesis(t *testing.T) {
	for _, userCancels := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete_partial_report", true: "user_cancellation"}[userCancels], func(t *testing.T) {
			r, initial := engineCollaboration(t)
			r.Lease = 300 * time.Millisecond
			b, err := r.AcquireParent(t.Context(), initial.Scope)
			require.NoError(t, err)
			_, err = r.Spawn(b.Context(t.Context()), b.Scope, "spawn", "research", "template", "{}", "task")
			require.NoError(t, err)
			requestCtx, cancel := context.WithCancel(t.Context())
			defer cancel()
			recoveryCtx, stop := b.Start(requestCtx)
			defer stop()
			model := &finalSynthesisChat{synthesize: func(ctx context.Context, _ []chat.Message, _ *chat.ChatOptions) (<-chan types.StreamResponse, error) {
				if userCancels {
					time.AfterFunc(30*time.Millisecond, cancel)
				}
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(450 * time.Millisecond):
					return finalSynthesisChunks(types.StreamResponse{ResponseType: types.ResponseTypeAnswer,
						Content: "slow partial report", Done: true, FinishReason: "stop"}), nil
				}
			}}
			engine := newTestEngine(t, model, withMaxIterations(1))
			engine.SetCollaboration(b)
			engine.toolRegistry = agenttools.NewToolRegistry()
			snapshot, err := json.Marshal(RunSnapshot{Version: RunSnapshotVersion,
				CollaborationWait: true, BetweenRounds: true, Query: "task", CurrentRound: 1})
			require.NoError(t, err)
			state, err := engine.Resume(recoveryCtx, "s", "m", nil, snapshot, nil)
			require.Len(t, model.finals, 1)
			if userCancels {
				require.ErrorIs(t, err, context.Canceled)
				return
			}
			require.NoError(t, err)
			require.Contains(t, state.FinalAnswer, "slow partial report")
			require.Contains(t, state.FinalAnswer, "阶段性结果")
		})
	}
}
