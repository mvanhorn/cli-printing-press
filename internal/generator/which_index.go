package generator

import (
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
)

// Novels occupy earlier slots so rankWhich declaration-order ties keep
// hero features ahead of promoted endpoint leaves. Command is the dedupe
// key; Promoted tags rows so dogfood sync can merge without wiping them.
type whichIndexEntry struct {
	Command      string
	Description  string
	Group        string
	WhyItMatters string
	Promoted     bool
}

// Novels stay first so rankWhich declaration-order ties prefer hero
// features; a promoted Command already claimed by a novel is skipped.
func (g *Generator) whichIndexEntries() []whichIndexEntry {
	if g == nil {
		return nil
	}
	entries := make([]whichIndexEntry, 0, len(g.NovelFeatures)+len(g.PromotedCommands))
	seen := map[string]bool{}
	for _, nf := range g.NovelFeatures {
		cmd := strings.TrimSpace(nf.Command)
		if cmd == "" || seen[cmd] {
			continue
		}
		seen[cmd] = true
		entries = append(entries, whichIndexEntry{
			Command:      cmd,
			Description:  strings.TrimSpace(nf.Description),
			Group:        strings.TrimSpace(nf.Group),
			WhyItMatters: strings.TrimSpace(nf.WhyItMatters),
		})
	}
	for _, pc := range g.PromotedCommands {
		cmd := strings.TrimSpace(pc.PromotedName)
		if cmd == "" || seen[cmd] {
			continue
		}
		seen[cmd] = true
		desc := naming.OneLine(pc.Endpoint.Description)
		if desc == "" && g.Spec != nil {
			if resource, ok := g.Spec.Resources[pc.ResourceName]; ok {
				desc = naming.OneLine(resource.Description)
			}
		}
		if desc == "" {
			desc = cmd
		}
		entries = append(entries, whichIndexEntry{
			Command:      cmd,
			Description:  desc,
			Group:        toKebab(pc.ResourceName),
			WhyItMatters: firstSentence(desc),
			Promoted:     true,
		})
	}
	return entries
}

func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for i, r := range s {
		if r == '.' || r == '!' || r == '?' {
			end := i + 1
			return strings.TrimSpace(s[:end])
		}
	}
	return s
}
