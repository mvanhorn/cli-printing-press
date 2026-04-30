package pipeline

import (
	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
)

// MergeOverlay applies an overlay onto an APISpec, modifying it in place.
// Non-nil overlay fields override the original spec values.
func MergeOverlay(s *spec.APISpec, overlay *SpecOverlay) {
	if overlay == nil || s == nil {
		return
	}

	for rName, rOverlay := range overlay.Resources {
		resource, ok := s.Resources[rName]
		if !ok {
			continue
		}

		if rOverlay.Description != nil {
			resource.Description = *rOverlay.Description
		}

		for eName, eOverlay := range rOverlay.Endpoints {
			endpoint, ok := resource.Endpoints[eName]
			if !ok {
				// Check sub-resources
				for subName, sub := range resource.SubResources {
					if ep, ok := sub.Endpoints[eName]; ok {
						if eOverlay.Description != nil {
							ep.Description = *eOverlay.Description
						}
						applyParamPatches(&ep, eOverlay.Params)
						sub.Endpoints[eName] = ep
						resource.SubResources[subName] = sub
						break
					}
				}
				continue
			}

			if eOverlay.Description != nil {
				endpoint.Description = *eOverlay.Description
			}
			applyParamPatches(&endpoint, eOverlay.Params)
			resource.Endpoints[eName] = endpoint
		}

		s.Resources[rName] = resource
	}
}

func applyParamPatches(endpoint *spec.Endpoint, patches []ParamPatch) {
	for _, patch := range patches {
		for i, param := range endpoint.Params {
			if param.Name == patch.Name {
				if patch.Default != nil {
					endpoint.Params[i].Default = *patch.Default
				}
				break
			}
		}
	}
}
