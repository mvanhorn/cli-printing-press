package generator

import (
	"fmt"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// dedupeFlagIdentifiers ensures that no two non-positional params or body
// fields on a single endpoint share a Go identifier (flag<Camel> /
// body<Camel>) or cobra flag name after camelization or kebab-casing, and
// that no entry collides with a reserved generator-introduced identifier
// (pagination's flagAll, async's flagWait*, mutating endpoints' --stdin).
//
// Conflicting entries have IdentName populated to Name with _2, _3, ...
// suffixed until the Go identifier and cobra flag name are both unique
// relative to other entries on the same endpoint and to reserved generator
// names. Without this, specs that use date-range filter conventions (e.g.,
// Twilio's StartTime, StartTime>, StartTime< all camelize to "StartTime"),
// expose a literal "all" parameter on a paginated endpoint (e.g., GitHub
// notifications colliding with pagination's --all), or have query params
// and body fields that share names produce duplicate `var flagX` / `var
// bodyX` declarations and refuse to compile, or register the same cobra
// flag twice and fail at runtime.
func (g *Generator) dedupeFlagIdentifiers() error {
	if g.Spec == nil {
		return nil
	}
	for resName, res := range g.Spec.Resources {
		for epName, ep := range res.Endpoints {
			deduped, err := dedupeEndpointIdentifiers(resName, epName, ep, g.AsyncJobs)
			if err != nil {
				return err
			}
			res.Endpoints[epName] = deduped
		}
		for subName, sub := range res.SubResources {
			// Sub-resource async lookups elsewhere in the generator (see
			// generator.go:1195) key on subName/epName without the parent
			// resource prefix; mirror that here so any future async
			// detection on sub-resources protects the wait identifiers
			// correctly. DetectAsyncJobs does not currently walk
			// sub-resources, so this lookup is a no-op today.
			for epName, ep := range sub.Endpoints {
				deduped, err := dedupeEndpointIdentifiers(subName, epName, ep, g.AsyncJobs)
				if err != nil {
					return err
				}
				sub.Endpoints[epName] = deduped
			}
			res.SubResources[subName] = sub
		}
		g.Spec.Resources[resName] = res
	}
	return nil
}

// dedupeEndpointIdentifiers runs the param-then-body uniquification for one
// endpoint, sharing the cobra flag-name namespace across both passes. Body
// fields and query/path params each emit `cmd.Flags().*Var(..., flagName, ...)`
// against the same cobra command, so collisions across the two lists must be
// detected together.
func dedupeEndpointIdentifiers(resKey, epName string, ep spec.Endpoint, asyncJobs map[string]AsyncJobInfo) (spec.Endpoint, error) {
	flagIdents, flagNames := reservedFlagNamesForEndpoint(resKey, epName, ep, asyncJobs)
	if err := validateAuthoredPublicFlags(resKey, epName, ep, flagNames); err != nil {
		return ep, err
	}

	// Pass 1: query/path params populate the flag<Camel> namespace.
	ep.Params = uniquifyIdentifiers(ep.Params, "flag", flagIdents, flagNames)

	// Pass 2: body fields populate the body<Camel> namespace, but their cobra
	// flag names share the namespace with everything we just registered.
	bodyFlagNames := make(map[string]struct{}, len(flagNames)+len(ep.Params))
	for k := range flagNames {
		bodyFlagNames[k] = struct{}{}
	}
	for _, p := range ep.Params {
		if !p.Positional {
			bodyFlagNames[publicFlagName(p)] = struct{}{}
		}
	}
	ep.Body = uniquifyIdentifiers(ep.Body, "body", nil, bodyFlagNames)

	return ep, nil
}

// reservedFlagNamesForEndpoint returns identifiers and cobra flag names that
// the command templates emit themselves and that user params or body fields
// therefore must not shadow. The returned `idents` set is in the flag<Camel>
// namespace (params); body<Camel> body-namespace identifiers carry no
// reserved entries because the generator-introduced helpers (stdinBody) use
// a different naming pattern. The `flags` set covers cobra flag names, which
// params and body fields share.
func reservedFlagNamesForEndpoint(resKey, epName string, ep spec.Endpoint, asyncJobs map[string]AsyncJobInfo) (idents, flags map[string]struct{}) {
	idents = map[string]struct{}{}
	flags = map[string]struct{}{}
	if ep.Pagination != nil {
		idents["flagAll"] = struct{}{}
		flags["all"] = struct{}{}
	}
	if _, isAsync := asyncJobs[resKey+"/"+epName]; isAsync {
		idents["flagWait"] = struct{}{}
		idents["flagWaitTimeout"] = struct{}{}
		idents["flagWaitInterval"] = struct{}{}
		flags["wait"] = struct{}{}
		flags["wait-timeout"] = struct{}{}
		flags["wait-interval"] = struct{}{}
	}
	switch ep.Method {
	case "POST", "PUT", "PATCH":
		// command_endpoint.go.tmpl:525 emits cmd.Flags().BoolVar(&stdinBody,
		// "stdin", ...) for mutating methods. stdinBody as a Go identifier
		// does not pattern-match flag<X> or body<X>, so no ident reservation
		// is needed; only the cobra flag name is shared.
		flags["stdin"] = struct{}{}
	}
	return idents, flags
}

// uniquifyIdentifiers returns params with IdentName populated whenever an
// entry's Go identifier (identPrefix + Camel(.Name)) or cobra flag name would
// otherwise collide with another entry earlier in the list or with a reserved
// generator name. The first occurrence of each colliding pattern keeps
// IdentName empty (templates fall back to Name); subsequent ones get
// IdentName set to Name with _2, _3, ... appended. Wire-side serialization
// always reads from Name and is never mutated. Positional params are not
// flagged and pass through.
//
// identPrefix is "flag" for query/path params and "body" for request body
// fields; the prefix selects the Go-identifier namespace. The cobra flag-name
// namespace is shared across both prefixes, so callers seed reservedFlags
// with names already registered by an earlier pass.
func uniquifyIdentifiers(params []spec.Param, identPrefix string, reservedIdents, reservedFlags map[string]struct{}) []spec.Param {
	if len(params) == 0 {
		return params
	}
	usedIdents := map[string]struct{}{}
	usedFlags := map[string]struct{}{}
	for k := range reservedIdents {
		usedIdents[k] = struct{}{}
	}
	for k := range reservedFlags {
		usedFlags[k] = struct{}{}
	}

	out := make([]spec.Param, len(params))
	for i, p := range params {
		if p.Positional {
			out[i] = p
			continue
		}
		ident := identPrefix + toCamel(p.Name)
		flag := publicFlagName(p)
		if _, identTaken := usedIdents[ident]; !identTaken {
			if _, flagTaken := usedFlags[flag]; !flagTaken {
				usedIdents[ident] = struct{}{}
				usedFlags[flag] = struct{}{}
				out[i] = p
				continue
			}
		}
		for n := 2; ; n++ {
			candidate := fmt.Sprintf("%s_%d", p.Name, n)
			ident = identPrefix + toCamel(candidate)
			flag = flagName(candidate)
			_, identTaken := usedIdents[ident]
			_, flagTaken := usedFlags[flag]
			if !identTaken && !flagTaken {
				p.IdentName = candidate
				usedIdents[ident] = struct{}{}
				usedFlags[flag] = struct{}{}
				out[i] = p
				break
			}
		}
	}
	return out
}

func validateAuthoredPublicFlags(resKey, epName string, ep spec.Endpoint, reservedFlags map[string]struct{}) error {
	seen := map[string]publicFlagUse{}
	for _, entry := range publicFlagEntries(ep.Params, "param") {
		if err := validatePublicFlagEntry(resKey, epName, entry, reservedFlags, seen); err != nil {
			return err
		}
	}
	for _, entry := range publicFlagEntries(ep.Body, "body") {
		if err := validatePublicFlagEntry(resKey, epName, entry, reservedFlags, seen); err != nil {
			return err
		}
	}
	return nil
}

type publicFlagUse struct {
	label    string
	explicit bool
}

type publicFlagEntry struct {
	name     string
	label    string
	explicit bool
}

func publicFlagEntries(params []spec.Param, kind string) []publicFlagEntry {
	entries := []publicFlagEntry{}
	for _, p := range params {
		public := publicFlagName(p)
		nameExplicit := strings.TrimSpace(p.FlagName) != ""
		entries = append(entries, publicFlagEntry{
			name:     public,
			label:    fmt.Sprintf("%s %q public name", kind, p.Name),
			explicit: nameExplicit,
		})
		for _, alias := range p.Aliases {
			entries = append(entries, publicFlagEntry{
				name:     alias,
				label:    fmt.Sprintf("%s %q alias", kind, p.Name),
				explicit: true,
			})
		}
	}
	return entries
}

func validatePublicFlagEntry(resKey, epName string, entry publicFlagEntry, reservedFlags map[string]struct{}, seen map[string]publicFlagUse) error {
	if entry.explicit {
		if _, ok := reservedFlags[entry.name]; ok {
			return fmt.Errorf("resource %q endpoint %q: %s %q collides with reserved flag --%s", resKey, epName, entry.label, entry.name, entry.name)
		}
	}
	if previous, ok := seen[entry.name]; ok {
		if entry.explicit || previous.explicit {
			return fmt.Errorf("resource %q endpoint %q: %s %q collides with %s", resKey, epName, entry.label, entry.name, previous.label)
		}
	}
	seen[entry.name] = publicFlagUse{label: entry.label, explicit: entry.explicit}
	return nil
}

// paramIdent returns the name a Param should use when deriving Go
// identifiers (via camel) or cobra flag names (via flagName). It is
// IdentName when populated by the dedup pass and Name otherwise. The
// resulting string must never be used for wire-side serialization;
// callers writing URL params, JSON keys, or path substitutions read
// Name directly.
func paramIdent(p spec.Param) string {
	if p.IdentName != "" {
		return p.IdentName
	}
	return p.Name
}

func publicFlagName(p spec.Param) string {
	if p.FlagName != "" {
		return p.FlagName
	}
	return flagName(paramIdent(p))
}

func publicFlagAliases(p spec.Param) []string {
	return p.Aliases
}
