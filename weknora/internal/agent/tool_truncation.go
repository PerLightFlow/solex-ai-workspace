package agent

import (
	"encoding/json"
	"fmt"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/types"
)

// A refusal never writes partial content. Metadata is for the UI; the full
// error is sent back to the model so it can retry only the missing chunk.
func (e *AgentEngine) truncatedToolResult(tc types.LLMToolCall) *types.ToolResult {
	result := &types.ToolResult{Success: false, Error: truncatedArgumentsError,
		Data: map[string]interface{}{"error_code": "tool_arguments_truncated"}}
	if tc.Function.Name != agenttools.ToolWriteSandboxFile {
		return result
	}
	limit := max(256, min(4096, len(tc.Function.Arguments)/2))
	for {
		old := e.fileWriteChunkLimit.Load()
		if old > 0 {
			limit = max(256, min(limit, int(old)/2))
		}
		if e.fileWriteChunkLimit.CompareAndSwap(old, int64(limit)) {
			break
		}
	}
	result.Data["retry_content_bytes"] = limit
	// Repair is used only to recover a display path, never to execute the
	// incomplete arguments or trust any partial file content.
	repaired, _ := agenttools.RepairJSONDetail(tc.Function.Arguments)
	var args struct {
		Path string `json:"path"`
	}
	if json.Unmarshal([]byte(repaired), &args) == nil && args.Path != "" {
		result.Data["file_path"] = args.Path
	}
	result.Error += "\n" + e.fileWriteRetryNotice()
	return result
}

func (e *AgentEngine) fileWriteRetryNotice() string {
	limit := e.fileWriteChunkLimit.Load()
	if limit == 0 {
		return ""
	}
	return fmt.Sprintf("File-write recovery for this run: send at most %d UTF-8 bytes of content per write_sandbox_file call. "+
		"The refused call wrote nothing. Retry ONLY its missing content, with path first and complete JSON. "+
		"Preserve successfully written chunks: use mode=append for subsequent chunks, never overwrite the whole file to retry. "+
		"If creating a new file, use overwrite only for its first successful chunk. Keep writes sequential. "+
		"Separate table data from a short reusable script; do not repeat all rows inside a large script. "+
		"Read back and validate the completed file before running it or claiming delivery.", limit)
}
