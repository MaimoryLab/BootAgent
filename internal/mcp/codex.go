package mcp

type CodexAdapter struct{ TOMLAdapter }

func NewCodexAdapter() Adapter { return CodexAdapter{TOMLAdapter{Section: "mcp_servers"}} }
