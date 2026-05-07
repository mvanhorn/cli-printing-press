package pipeline

import (
	"fmt"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// MergeOverlay applies an overlay onto an APISpec, modifying it in place.
// Non-nil overlay fields override the original spec values.
func MergeOverlay(s *spec.APISpec, overlay *SpecOverlay) error {
	if overlay == nil || s == nil {
		return nil
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
						if err := applyParamPatches(&ep.Params, eOverlay.Params); err != nil {
							return fmt.Errorf("resource %q sub-resource %q endpoint %q params: %w", rName, subName, eName, err)
						}
						if err := applyParamPatches(&ep.Body, eOverlay.Body); err != nil {
							return fmt.Errorf("resource %q sub-resource %q endpoint %q body: %w", rName, subName, eName, err)
						}
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
			if err := applyParamPatches(&endpoint.Params, eOverlay.Params); err != nil {
				return fmt.Errorf("resource %q endpoint %q params: %w", rName, eName, err)
			}
			if err := applyParamPatches(&endpoint.Body, eOverlay.Body); err != nil {
				return fmt.Errorf("resource %q endpoint %q body: %w", rName, eName, err)
			}
			resource.Endpoints[eName] = endpoint
		}

		s.Resources[rName] = resource
	}
	return nil
}

func applyParamPatches(params *[]spec.Param, patches []ParamPatch) error {
	for _, patch := range patches {
		if patch.FlagName != nil && *patch.FlagName == "" {
			return fmt.Errorf("param %q: flag_name must not be empty; use clear_flag_name to remove it", patch.Name)
		}
		for i, param := range *params {
			if param.Name == patch.Name {
				if patch.Default != nil {
					(*params)[i].Default = *patch.Default
				}
				if patch.ClearFlagName {
					(*params)[i].FlagName = ""
				}
				if patch.FlagName != nil {
					(*params)[i].FlagName = *patch.FlagName
				}
				if patch.Aliases != nil {
					(*params)[i].Aliases = append([]string(nil), (*patch.Aliases)...)
				}
				break
			}
		}
	}
	return nil
}
