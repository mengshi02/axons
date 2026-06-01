/**
 * Backend Agent Client
 *
 * Calls the backend Agent API instead of using LangChain directly.
 * This keeps API keys secure on the server side.
 */

import type { AgentStreamChunk } from './types';

/**
 * Agent request
 */
interface AgentRequest {
  session_id: string;
  message: string;
  context?: string;
  agent_id?: string;
  project_id?: string;
  model_id?: string; // selected model ID
  images?: string[]; // base64 dataUrl list for multimodal
}

/**
 * Agent event from SSE stream
 */
interface AgentEvent {
  type: 'token' | 'tool_start' | 'tool_end' | 'done' | 'error' | 'thinking' | 'heartbeat';
  content?: string;
  tool_name?: string;
  tool_args?: Record<string, unknown>;
  tool_result?: string;
  duration_ms?: number; // Tool execution duration in milliseconds
  error?: string;
  error_type?: string; // Error type: rate_limit, auth_error, server_error, unknown
  retryable?: boolean; // Whether the error is retryable
  modified_files?: string[]; // Files modified by tool (e.g., write_file, replace_file)
}

/**
 * Generate a unique session ID
 */
function generateSessionId(): string {
  return `session-${Date.now()}-${Math.random().toString(36).substring(2, 9)}`;
}

/**
 * Chat with the backend agent and get a streaming response
 *
 * This function calls the backend Agent API which:
 * - Keeps API keys secure on the server
 * - Uses MCP tools for code analysis
 * - Maintains conversation memory
 */
export async function chatWithAgent(
  message: string,
  onChunk: (chunk: AgentStreamChunk) => void,
  sessionId?: string,
  agentId?: string,
  projectId?: string,
  images?: string[],
  modelId?: string,
  signal?: AbortSignal,
): Promise<void> {
  const sid = sessionId || generateSessionId();

  const request: AgentRequest = {
    session_id: sid,
    message,
    agent_id: agentId,
    project_id: projectId,
    model_id: modelId,
    images: images && images.length > 0 ? images : undefined,
  };

  try {
    const response = await fetch('/api/chat/stream', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(request),
      signal,
    });

    if (!response.ok) {
      const errorData = await response.json().catch(() => ({}));
      throw new Error(errorData.message || `Agent request failed: ${response.status}`);
    }

    const reader = response.body?.getReader();
    if (!reader) {
      throw new Error('No response body');
    }

    const decoder = new TextDecoder();
    let buffer = '';

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      buffer += decoder.decode(value, { stream: true });

      // Process complete SSE events
      const lines = buffer.split('\n');
      buffer = lines.pop() || ''; // Keep incomplete line in buffer

      for (const line of lines) {
        if (line.startsWith('data: ')) {
          const data = line.slice(6).trim();
          if (data === '[DONE]') {
            onChunk({ type: 'done' });
            return;
          }

          try {
            const event: AgentEvent = JSON.parse(data);

            switch (event.type) {
              case 'token':
                if (event.content) {
                  onChunk({ type: 'token', content: event.content });
                }
                break;

              case 'tool_start':
                if (event.tool_name) {
                  onChunk({
                    type: 'tool_start',
                    toolName: event.tool_name,
                    toolInput: event.tool_args,
                  });
                }
                break;

              case 'tool_end':
                if (event.tool_name) {
                  onChunk({
                    type: 'tool_end',
                    toolName: event.tool_name,
                    toolOutput: event.tool_result,
                    durationMs: event.duration_ms,
                    modifiedFiles: event.modified_files,
                  });
                }
                break;

              case 'thinking':
                onChunk({
                  type: 'thinking',
                  content: event.content,
                });
                break;

              case 'error':
                onChunk({
                  type: 'error',
                  error: event.error || 'Unknown error',
                  errorType: event.error_type,
                  retryable: event.retryable,
                });
                break;

              case 'done':
                onChunk({ type: 'done' });
                return;

              case 'heartbeat':
                // Ignore heartbeat events - they just keep the connection alive
                break;
            }
          } catch {
            // Ignore parse errors for incomplete events
          }
        }
      }
    }

    // Process any remaining buffer
    if (buffer.startsWith('data: ')) {
      const data = buffer.slice(6).trim();
      if (data === '[DONE]') {
        onChunk({ type: 'done' });
      } else {
        try {
          const event: AgentEvent = JSON.parse(data);
          if (event.type === 'done') {
            onChunk({ type: 'done' });
          }
        } catch {
          // Ignore parse errors
        }
      }
    }
  } catch (error) {
    onChunk({
      type: 'error',
      error: error instanceof Error ? error.message : 'Unknown error',
    });
  }
}

/**
 * Non-streaming chat (simple request-response)
 */
export async function chatSimple(
  message: string,
  sessionId?: string
): Promise<string> {
  const sid = sessionId || generateSessionId();

  const response = await fetch('/api/chat', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      session_id: sid,
      message,
    }),
  });

  if (!response.ok) {
    const errorData = await response.json().catch(() => ({}));
    throw new Error(errorData.message || `Chat request failed: ${response.status}`);
  }

  const data = await response.json();
  return data.content || '';
}

/**
 * Clear conversation history for a session
 */
export async function clearSession(sessionId: string): Promise<void> {
  await fetch('/api/chat/clear', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ session_id: sessionId }),
  });
}

/**
 * Check if the agent is available
 */
export async function checkAgentAvailability(): Promise<boolean> {
  try {
    const response = await fetch('/api/chat', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ session_id: 'test', message: 'test' }),
    });
    // If we get a 503 (service unavailable), agent is not configured
    if (response.status === 503) {
      return false;
    }
    return true;
  } catch {
    return false;
  }
}