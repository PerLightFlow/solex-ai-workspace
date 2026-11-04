import assert from 'node:assert/strict'
import test from 'node:test'
import { isTruncatedToolCall, toolTruncationStatus } from './toolTruncation'

const failed = { type: 'tool_call', tool_call_id: 'a', tool_name: 'write_sandbox_file', success: false,
  tool_data: { error_code: 'tool_arguments_truncated', file_path: '/workspace/a.py' } }
const success = { type: 'tool_call', tool_call_id: 'b', tool_name: 'write_sandbox_file', success: true,
  arguments: { path: '/workspace/a.py', mode: 'append' } }

test('cutoff states distinguish active recovery, resumed writes and unresolved final calls', () => {
  assert.equal(toolTruncationStatus(failed, [failed], false), 'retrying')
  assert.equal(toolTruncationStatus(failed, [failed], true), 'failed')
  assert.equal(toolTruncationStatus(failed, [failed, success], true), 'recovered')
  assert.equal(toolTruncationStatus(failed, [success, failed], true), 'failed')
  assert.equal(toolTruncationStatus(failed, [failed, { ...success, arguments: { path: '/workspace/b.py' } }], true), 'failed')
  assert.equal(isTruncatedToolCall({ ...failed, success: true }), false)
})

test('legacy incomplete arguments are read only for path matching', () => {
  const old = { ...failed, tool_data: undefined, output: 'Tool call was not executed: the model output was cut off before the arguments finished.',
    arguments: { _raw: '{"path":"/workspace/a.py","content":"unfinished' } }
  assert.equal(toolTruncationStatus(old, [old, success], true), 'recovered')
  assert.equal(toolTruncationStatus({ ...old, arguments: {} }, [old, success], true), 'failed')
  assert.equal(toolTruncationStatus({ ...old, tool_name: 'search' }, [old, success], true), 'failed')
  assert.equal(isTruncatedToolCall({ success: false, output: 'Network error' }), false)
})
