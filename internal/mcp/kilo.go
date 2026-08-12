package mcp

type KiloAdapter struct{ JSONAdapter }

func NewKiloAdapter() Adapter { return KiloAdapter{JSONAdapter{Section: "mcp"}} }
