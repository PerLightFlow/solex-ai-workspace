type ToolEvent = {
  type?: string; tool_call_id?: string; tool_name?: string; success?: boolean;
  output?: string; error?: string; arguments?: any; tool_data?: any;
}

export function isTruncatedToolCall(event: ToolEvent): boolean {
  return event.success === false && (event.tool_data?.error_code === 'tool_arguments_truncated' ||
    /Tool call was not executed: the model output was cut off/.test(event.error || event.output || ''))
}

function filePath(event: ToolEvent): string | undefined {
  const direct = event.tool_data?.file_path || event.arguments?.path || event.tool_data?.path
  if (typeof direct === 'string' && direct) return direct
  // Historical records only have partial arguments. Read the complete path
  // string if present; never repair or evaluate the partial file contents.
  const raw = event.arguments?._raw
  if (typeof raw !== 'string') return undefined
  const match = raw.match(/^\s*\{\s*"path"\s*:\s*("(?:[^"\\]|\\.)*")/)
  try { return match ? JSON.parse(match[1]) : undefined } catch { return undefined }
}

export function toolTruncationStatus(event: ToolEvent, events: ToolEvent[], done: boolean): 'retrying' | 'recovered' | 'failed' | undefined {
  if (!isTruncatedToolCall(event)) return undefined
  const index = events.findIndex(e => e === event || (!!event.tool_call_id && e.tool_call_id === event.tool_call_id))
  const path = event.tool_name === 'write_sandbox_file' ? filePath(event) : undefined
  if (path && index >= 0 && events.slice(index + 1).some(e => e.type === 'tool_call' &&
    e.tool_name === event.tool_name && e.success === true && filePath(e) === path)) return 'recovered'
  return done ? 'failed' : 'retrying'
}
