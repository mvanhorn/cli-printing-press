package generator

import (
	"github.com/mvanhorn/cli-printing-press/v3/internal/vision"
)

// VisionTemplateSet defines which visionary templates to include in generation.
type VisionTemplateSet struct {
	Export    bool
	Import    bool
	Store     bool
	Search    bool
	Sync      bool
	Tail      bool
	Analytics bool
	MCP       bool
	Workflows []string
	Insights  []string
}

// CmdNames returns a set of command names that the VisionSet registers in
// root.go. Used to exclude these from the Resources loop to prevent duplicates
// when an API has an endpoint with the same name (e.g., /analytics).
func (s VisionTemplateSet) CmdNames() map[string]bool {
	names := map[string]bool{}
	if s.Export {
		names["export"] = true
	}
	if s.Import {
		names["import"] = true
	}
	if s.Search {
		names["search"] = true
	}
	if s.Sync {
		names["sync"] = true
	}
	if s.Tail {
		names["tail"] = true
	}
	if s.Analytics {
		names["analytics"] = true
	}
	return names
}

func (s VisionTemplateSet) IsZero() bool {
	return !s.Export && !s.Import && !s.Store && !s.Search &&
		!s.Sync && !s.Tail && !s.Analytics && !s.MCP &&
		len(s.Workflows) == 0 && len(s.Insights) == 0
}

// SelectVisionTemplates determines which domain-aware templates to include
// based on the visionary research plan's architecture decisions and feature scores.
func SelectVisionTemplates(plan *vision.VisionaryPlan) VisionTemplateSet {
	if plan == nil {
		return VisionTemplateSet{}
	}

	set := VisionTemplateSet{
		// Export and Import are always available (low cost, high utility)
		Export: true,
		Import: true,
	}

	// Check architecture decisions for persistence and search needs
	for _, ad := range plan.Architecture {
		switch ad.Area {
		case "persistence":
			if ad.NeedLevel == "high" || ad.NeedLevel == "medium" {
				set.Store = true
				set.Sync = true
			}
		case "search":
			if ad.NeedLevel == "high" {
				set.Search = true
				set.Store = true // Search requires store
			}
		case "realtime":
			if ad.NeedLevel == "high" || ad.NeedLevel == "medium" {
				set.Tail = true
			}
		}
	}

	// Check data profile
	dp := plan.Identity.DataProfile
	if dp.Volume == "high" {
		set.Store = true
		set.Analytics = true
	}
	if dp.SearchNeed == "high" {
		set.Search = true
		set.Store = true
	}
	if dp.Realtime {
		set.Tail = true
	}

	// Check feature scores - any feature scoring 8+ that references a template
	for _, f := range plan.Features {
		score := f.ComputeScore()
		if score < 8 {
			continue
		}
		for _, tmpl := range f.TemplateNames {
			switch tmpl {
			case "export.go.tmpl":
				set.Export = true
			case "import.go.tmpl":
				set.Import = true
			case "store.go.tmpl":
				set.Store = true
			case "search.go.tmpl":
				set.Search = true
				set.Store = true
			case "sync.go.tmpl":
				set.Sync = true
				set.Store = true
			case "tail.go.tmpl":
				set.Tail = true
			case "analytics.go.tmpl":
				set.Analytics = true
				set.Store = true
			}
		}
	}

	switch plan.Domain.Archetype {
	case "project-management":
		set.Workflows = []string{
			"workflows/pm_stale.go.tmpl",
			"workflows/pm_orphans.go.tmpl",
			"workflows/pm_load.go.tmpl",
		}
	case "communication":
		set.Workflows = []string{
			"workflows/comm_health.go.tmpl",
		}
	}

	// Invariant: a store without sync is useless — sync populates the store.
	if set.Store && !set.Sync {
		set.Sync = true
	}

	// MCP server is always generated alongside the CLI
	set.MCP = true

	if plan.Insight.HasInsight() {
		set.Insights = []string{
			"insights/health_score.go.tmpl",
			"insights/similar.go.tmpl",
		}
	}

	return set
}

func (s VisionTemplateSet) HasWorkflows() bool {
	return len(s.Workflows) > 0
}

func (s VisionTemplateSet) HasInsights() bool {
	return len(s.Insights) > 0
}

// TemplateNames returns the list of template filenames to render.
func (s VisionTemplateSet) TemplateNames() []string {
	var names []string
	if s.Export {
		names = append(names, "export.go.tmpl")
	}
	if s.Import {
		names = append(names, "import.go.tmpl")
	}
	if s.Store {
		names = append(names, "store.go.tmpl")
	}
	if s.Search {
		names = append(names, "search.go.tmpl")
	}
	if s.Sync {
		names = append(names, "sync.go.tmpl")
	}
	if s.Tail {
		names = append(names, "tail.go.tmpl")
	}
	if s.Analytics {
		names = append(names, "analytics.go.tmpl")
	}
	names = append(names, s.Workflows...)
	names = append(names, s.Insights...)
	return names
}
