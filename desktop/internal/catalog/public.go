package catalog

import "sort"

// Group is a catalog section. Ordered as declared, because the order is what the
// overview shows.
type Group struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Groups are the sections the catalog is presented in.
var Groups = []Group{
	{ID: "auto", Name: "One-click configurable"},
	{ID: "gateway", Name: "Gateway agents"},
	{ID: "platform", Name: "Official account agents"},
	{ID: "ide", Name: "IDE extensions"},
}

// PublicAgent is one catalog entry as the frontend sees it.
//
// Projected field by field rather than serialising catalog.Agent: that struct
// carries the pinned package's integrity, source and command, none of which the
// client needs, and sending it wholesale is how an internal field leaks the moment
// it is added.
type PublicAgent struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Group         string   `json:"group"`
	ConfigMode    string   `json:"configMode"`
	GuideOnly     bool     `json:"guideOnly"`
	LockedVersion *string  `json:"lockedVersion"`
	Protocol      *string  `json:"protocol"`
	Platforms     []string `json:"platforms"`
	// PlatformNote is only shown on the platform it applies to, so a Windows note
	// does not appear on macOS where it would be misleading.
	//
	// Keyed on the host OS rather than the runtime's os_id, which is what the
	// Python core does. Those differ only when a runtime is constructed for another
	// platform -- which happens in tests, not in production -- so the distinction
	// is invisible to users, but it is the shipped behaviour and the migration does
	// not change observable output while proving equivalence. Worth revisiting once
	// Python is gone.
	PlatformNote string `json:"platformNote"`
	// Rank is how prominently to show the Agent, independent of whether OneAgent
	// can install it: an overview is judged by whether the tools people actually
	// use are on it.
	Rank int `json:"rank"`
}

// PublicCatalog projects the manifest for the frontend, in the one order every
// client shows so none of them re-derives it.
//
// osID decides whether the Windows note is shown. Callers pass the host OS, not a
// runtime's simulated one -- see PlatformNote.
func (m *Manifest) PublicCatalog(osID string) []PublicAgent {
	items := make([]PublicAgent, 0, len(m.Agents))
	for id, agent := range m.Agents {
		item := PublicAgent{
			ID: id, Name: agent.Name, Group: agent.Group,
			ConfigMode: agent.ConfigMode, GuideOnly: agent.GuideOnly(),
			Platforms: agent.Platforms, Rank: agent.RankOrDefault(),
		}
		if agent.Platforms == nil {
			item.Platforms = []string{}
		}
		if agent.Package != nil {
			version := agent.Package.Version
			item.LockedVersion = &version
		}
		if !agent.GuideOnly() {
			protocol := AgentProtocol(agent.ConfigAdapter)
			item.Protocol = &protocol
		}
		if osID == "windows" {
			item.PlatformNote = agent.WindowsNote
		}
		items = append(items, item)
	}
	sort.Slice(items, func(first, second int) bool {
		if items[first].Rank != items[second].Rank {
			return items[first].Rank < items[second].Rank
		}
		return items[first].ID < items[second].ID
	})
	return items
}
