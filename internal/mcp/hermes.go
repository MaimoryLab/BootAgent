package mcp

type HermesAdapter struct{ YAMLAdapter }

func NewHermesAdapter() Adapter { return HermesAdapter{YAMLAdapter{Section: "mcp_servers"}} }
