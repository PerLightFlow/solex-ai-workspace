package agent

import (
	"context"
	"strings"
	"testing"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestFileTruncationShrinksRetryAndPreservesSuccessfulChunks(t *testing.T) {
	cut := types.LLMToolCall{ID: "cut", Function: types.FunctionCall{Name: "write_sandbox_file",
		Arguments: `{"path":"/workspace/report.py","mode":"append","content":"` + strings.Repeat("x", 2000)}}
	complete := types.LLMToolCall{ID: "ok", Function: types.FunctionCall{Name: "write_sandbox_file",
		Arguments: `{"path":"/workspace/report.py","mode":"append","content":"small"}`}}
	model := &mockChat{responses: []mockResponse{
		{chunks: []types.StreamResponse{{Done: true, FinishReason: "length", ToolCalls: []types.LLMToolCall{cut}}}},
		{chunks: []types.StreamResponse{{Done: true, FinishReason: "length", ToolCalls: []types.LLMToolCall{cut}}}},
		{chunks: []types.StreamResponse{{Done: true, FinishReason: "tool_calls", ToolCalls: []types.LLMToolCall{complete}}}},
		{chunks: []types.StreamResponse{{Done: true, FinishReason: "stop", Content: "done"}}},
	}}
	engine := newTestEngine(t, model)
	engine.toolRegistry = agenttools.NewToolRegistry()
	tool := newCountingTool("write_sandbox_file")
	engine.toolRegistry.RegisterTool(tool)
	state := &types.AgentState{}
	_, err := engine.executeLoop(context.Background(), state, "write the remaining file", emptyMessages(), emptyTools(), "s", "m")
	require.NoError(t, err)
	require.Equal(t, 1, tool.calls, "neither truncated call may touch the file")
	require.Len(t, state.RoundSteps, 4)
	first := state.RoundSteps[0].ToolCalls[0].Result
	second := state.RoundSteps[1].ToolCalls[0].Result
	require.Equal(t, "tool_arguments_truncated", first.Data["error_code"])
	require.Equal(t, "/workspace/report.py", first.Data["file_path"])
	require.Less(t, second.Data["retry_content_bytes"].(int), first.Data["retry_content_bytes"].(int))
	require.Contains(t, model.opts[1].RuntimeNotice, "Retry ONLY")
	require.Contains(t, model.opts[2].RuntimeNotice, "mode=append")
	require.Contains(t, model.opts[3].RuntimeNotice, "Read back and validate", "recovery guidance survives a successful chunk")
	require.True(t, state.IsComplete)
}

func TestTruncatedUnrelatedToolDoesNotChangeFilePolicy(t *testing.T) {
	engine := newTestEngine(t, &mockChat{})
	result := engine.truncatedToolResult(types.LLMToolCall{Function: types.FunctionCall{Name: "search", Arguments: `{"query":"a`}})
	require.Equal(t, "tool_arguments_truncated", result.Data["error_code"])
	require.Empty(t, engine.fileWriteRetryNotice())
}
