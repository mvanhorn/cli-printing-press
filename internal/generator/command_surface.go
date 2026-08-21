package generator

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

type commandSurfaceEntry struct {
	SpecPath    string
	CommandPath string
	Constructor string
	OutputPath  string
}

// collectionItemRole qualifies an item-scoped sub-resource when a collection
// endpoint already claimed the same leaf name. Vendored specs cannot rename
// either path, so the item side takes the qualifier and the collection stem stays.
const collectionItemRole = "item"

type subResourceCommandNames struct {
	fileStem         string
	rawStem          string
	constructorIdent string
	commandPath      string
	funcPrefix       string
	commandUse       string
}

func collectionItemCollisionLeaves(resource spec.Resource) map[string]bool {
	endpointLeaves := make(map[string]struct{}, len(resource.Endpoints))
	for name, endpoint := range resource.Endpoints {
		if strings.EqualFold(endpoint.Method, "OPTIONS") {
			continue
		}
		endpointLeaves[toKebab(name)] = struct{}{}
	}
	leaves := make(map[string]bool)
	for name := range resource.SubResources {
		if _, ok := endpointLeaves[toKebab(name)]; ok {
			leaves[name] = true
		}
	}
	return leaves
}

func subResourceCommandNamesFor(resourceName, subResourceName string, itemScoped bool) subResourceCommandNames {
	resourceCommand := toKebab(resourceName)
	if itemScoped {
		rawStem := resourceName + "_" + collectionItemRole + "_" + subResourceName
		commandUse := toKebab(collectionItemRole + "-" + subResourceName)
		return subResourceCommandNames{
			fileStem:         safeResourceFileStem(rawStem),
			rawStem:          rawStem,
			constructorIdent: commandIdent(resourceName, collectionItemRole, subResourceName),
			commandPath:      strings.Join([]string{resourceCommand, commandUse}, " "),
			funcPrefix:       resourceName + "-" + collectionItemRole + "-" + subResourceName,
			commandUse:       commandUse,
		}
	}
	rawStem := resourceName + "_" + subResourceName
	commandUse := toKebab(subResourceName)
	return subResourceCommandNames{
		fileStem:         safeResourceFileStem(rawStem),
		rawStem:          rawStem,
		constructorIdent: commandIdent(resourceName, subResourceName),
		commandPath:      strings.Join([]string{resourceCommand, commandUse}, " "),
		funcPrefix:       resourceName + "-" + subResourceName,
		commandUse:       commandUse,
	}
}

func subResourceCmdIdent(parentPrefix, subName string, resource spec.Resource) string {
	if collectionItemCollisionLeaves(resource)[subName] {
		return commandIdent(parentPrefix, collectionItemRole, subName)
	}
	return commandIdent(parentPrefix, subName)
}

func buildCommandSurface(apiSpec *spec.APISpec, promotedCommands []PromotedCommand) []commandSurfaceEntry {
	if apiSpec == nil {
		return nil
	}

	promotedByResource := make(map[string]PromotedCommand, len(promotedCommands))
	for _, command := range promotedCommands {
		promotedByResource[command.ResourceName] = command
	}

	resourceNames := make([]string, 0, len(apiSpec.Resources))
	for name := range apiSpec.Resources {
		resourceNames = append(resourceNames, name)
	}
	sort.Strings(resourceNames)

	var entries []commandSurfaceEntry
	for _, resourceName := range resourceNames {
		resource := withoutOptionsEndpoints(apiSpec.Resources[resourceName])
		resourceCommand := toKebab(resourceName)
		promotedCommand, promoted := promotedByResource[resourceName]
		if promoted {
			entries = append(entries, commandSurfaceEntry{
				SpecPath:    endpointSpecPath(resourceName, "", promotedCommand.EndpointName),
				CommandPath: resourceCommand,
				Constructor: "new" + commandIdent(promotedCommand.PromotedName) + "PromotedCmd",
				OutputPath:  filepath.Join("internal", "cli", safeResourceFileStem("promoted_"+promotedCommand.PromotedName)+".go"),
			})
		} else {
			entries = append(entries, commandSurfaceEntry{
				SpecPath:    resourceSpecPath(resourceName, ""),
				CommandPath: resourceCommand,
				Constructor: "new" + commandIdent(resourceName) + "Cmd",
				OutputPath:  filepath.Join("internal", "cli", safeResourceFileStem(resourceName)+".go"),
			})
		}
		collidingLeaves := collectionItemCollisionLeaves(resource)
		for _, endpointName := range sortedEndpointNames(resource.Endpoints) {
			if promoted && endpointName == promotedCommand.EndpointName {
				continue
			}
			entries = append(entries, commandSurfaceEntry{
				SpecPath:    endpointSpecPath(resourceName, "", endpointName),
				CommandPath: strings.Join([]string{resourceCommand, toKebab(endpointName)}, " "),
				Constructor: "new" + commandIdent(resourceName, endpointName) + "Cmd",
				OutputPath:  filepath.Join("internal", "cli", safeResourceFileStem(resourceName+"_"+endpointName)+".go"),
			})
		}

		subResourceNames := make([]string, 0, len(resource.SubResources))
		for name := range resource.SubResources {
			subResourceNames = append(subResourceNames, name)
		}
		sort.Strings(subResourceNames)
		for _, subResourceName := range subResourceNames {
			subResource := withoutOptionsEndpoints(resource.SubResources[subResourceName])
			naming := subResourceCommandNamesFor(resourceName, subResourceName, collidingLeaves[subResourceName])
			entries = append(entries, commandSurfaceEntry{
				SpecPath:    resourceSpecPath(resourceName, subResourceName),
				CommandPath: naming.commandPath,
				Constructor: "new" + naming.constructorIdent + "Cmd",
				OutputPath:  filepath.Join("internal", "cli", naming.fileStem+".go"),
			})
			for _, endpointName := range sortedEndpointNames(subResource.Endpoints) {
				entries = append(entries, commandSurfaceEntry{
					SpecPath:    endpointSpecPath(resourceName, subResourceName, endpointName),
					CommandPath: strings.Join([]string{naming.commandPath, toKebab(endpointName)}, " "),
					Constructor: "new" + commandIdent(naming.funcPrefix, endpointName) + "Cmd",
					OutputPath:  filepath.Join("internal", "cli", safeResourceFileStem(naming.rawStem+"_"+endpointName)+".go"),
				})
			}
		}
	}

	return entries
}

func validateCommandSurface(entries []commandSurfaceEntry, frameworkCommandNames map[string]struct{}) error {
	type collisionKey struct {
		kind  string
		value string
	}
	seen := make(map[collisionKey]commandSurfaceEntry, len(entries)*3)
	for _, entry := range entries {
		if !strings.Contains(entry.CommandPath, " ") {
			if _, reserved := frameworkCommandNames[entry.CommandPath]; reserved {
				return fmt.Errorf("derived command path %q for %s collides with an emitted framework command; rename the spec resource or endpoint",
					entry.CommandPath, entry.SpecPath)
			}
		}
		keys := []collisionKey{
			{kind: "output path", value: entry.OutputPath},
			{kind: "constructor", value: entry.Constructor},
			{kind: "command path", value: entry.CommandPath},
		}
		for _, key := range keys {
			if previous, ok := seen[key]; ok && previous.SpecPath != entry.SpecPath {
				return fmt.Errorf("derived command %s %q collides between %s and %s; rename one spec resource or endpoint",
					key.kind, key.value, previous.SpecPath, entry.SpecPath)
			}
			seen[key] = entry
		}
	}
	return nil
}

func expectedCommandPaths(entries []commandSurfaceEntry) []string {
	paths := make([]string, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if _, ok := seen[entry.CommandPath]; ok {
			continue
		}
		seen[entry.CommandPath] = struct{}{}
		paths = append(paths, entry.CommandPath)
	}
	sort.Strings(paths)
	return paths
}

func resourceSpecPath(resourceName, subResourceName string) string {
	if subResourceName == "" {
		return fmt.Sprintf("resource %q", resourceName)
	}
	return fmt.Sprintf("resource %q sub-resource %q", resourceName, subResourceName)
}

func endpointSpecPath(resourceName, subResourceName, endpointName string) string {
	if subResourceName == "" {
		return fmt.Sprintf("resource %q endpoint %q", resourceName, endpointName)
	}
	return fmt.Sprintf("resource %q sub-resource %q endpoint %q", resourceName, subResourceName, endpointName)
}
