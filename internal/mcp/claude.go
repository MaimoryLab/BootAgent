package mcp

type ClaudeAdapter struct{ JSONAdapter }

func NewClaudeAdapter() Adapter { return ClaudeAdapter{JSONAdapter{Section: "mcpServers"}} }
