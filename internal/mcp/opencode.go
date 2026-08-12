package mcp

type OpenCodeAdapter struct{ JSONAdapter }

func NewOpenCodeAdapter() Adapter { return OpenCodeAdapter{JSONAdapter{Section: "mcp"}} }
