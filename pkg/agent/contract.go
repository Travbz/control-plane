// Package agent defines the contract between the platform and agent binaries.
//
// An agent is a black box that receives a task, uses tools, calls LLMs,
// and produces a result. The platform doesn't care what language it's
// written in or how it works internally — only that it follows this contract.
//
// # Input
//
// The agent receives its task via one of:
//   - TASK_PAYLOAD environment variable (JSON string)
//   - stdin (JSON, if TASK_PAYLOAD is empty)
//
// # Environment Variables
//
// The platform injects these env vars into the sandbox:
//   - TASK_PAYLOAD: JSON-encoded TaskInput (if not using stdin)
//   - TOOL_ENDPOINTS: comma-separated list of "name=transport://addr" pairs
//   - ANTHROPIC_API_KEY / OPENAI_API_KEY: session token for LLM access
//   - ANTHROPIC_BASE_URL / OPENAI_BASE_URL: proxy URL for LLM calls
//   - SYSTEM_PROMPT: composed system prompt (base + customer + memories)
//
// # Output
//
// The agent writes a JSON-encoded TaskOutput to stdout on completion.
// Any stderr output is treated as logs.
//
// # Exit Codes
//
//   - 0: success — stdout contains a valid TaskOutput with status "completed"
//   - 1: agent error — stdout may contain a TaskOutput with status "failed"
//   - 2: task rejected — agent determined it cannot handle this task
//   - other: unexpected crash
package agent

// TaskInput is the JSON payload the agent receives as input.
type TaskInput struct {
	// TaskID is the unique identifier for this task.
	TaskID string `json:"task_id"`

	// Prompt is the user/customer prompt describing what to do.
	Prompt string `json:"prompt"`

	// SystemPrompt is the composed system prompt (base + customer + memories).
	// If empty, the agent should use its own default.
	SystemPrompt string `json:"system_prompt,omitempty"`

	// Tools lists the available MCP tool endpoints.
	// This is also available via TOOL_ENDPOINTS env var.
	Tools []ToolEndpoint `json:"tools,omitempty"`

	// Context is optional prior conversation or memory context.
	Context []ContextEntry `json:"context,omitempty"`

	// MaxIterations limits the number of tool-use loops. 0 = unlimited.
	MaxIterations int `json:"max_iterations,omitempty"`
}

// ToolEndpoint describes a single available tool.
type ToolEndpoint struct {
	// Name is the tool identifier.
	Name string `json:"name"`

	// Transport is "http" or "stdio".
	Transport string `json:"transport"`

	// Address is the endpoint (e.g. "http://echo:8080" or "stdio://echo").
	Address string `json:"address"`
}

// ContextEntry is a prior message or memory fragment.
type ContextEntry struct {
	Role    string `json:"role"`    // "user", "assistant", "system"
	Content string `json:"content"` // text content
}

// TaskOutput is the JSON payload the agent writes to stdout.
type TaskOutput struct {
	// TaskID matches the input TaskID.
	TaskID string `json:"task_id"`

	// Status is "completed", "failed", or "rejected".
	Status string `json:"status"`

	// Result is the final output text (for status "completed").
	Result string `json:"result,omitempty"`

	// Error is the error message (for status "failed").
	Error string `json:"error,omitempty"`

	// ToolCalls is the list of tool calls made during execution.
	ToolCalls []ToolCallRecord `json:"tool_calls,omitempty"`

	// TokensUsed is the total token count (if tracked by the agent).
	TokensUsed *TokenUsage `json:"tokens_used,omitempty"`
}

// ToolCallRecord records a single tool invocation.
type ToolCallRecord struct {
	Tool   string `json:"tool"`
	Input  string `json:"input"`
	Output string `json:"output"`
}

// TokenUsage tracks token consumption.
type TokenUsage struct {
	Input  int64 `json:"input"`
	Output int64 `json:"output"`
}

// Exit codes.
const (
	ExitSuccess  = 0
	ExitFailed   = 1
	ExitRejected = 2
)

// Status values for TaskOutput.
const (
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusRejected  = "rejected"
)
