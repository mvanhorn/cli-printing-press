package generator

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/shellargs"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

type RecipeIntent struct {
	Name        string
	Description string
	Command     []string
	Args        []RecipeIntentArg
	Params      []RecipeIntentParam
}

type RecipeIntentArg struct {
	Static bool
	Token  string
	Param  RecipeIntentParam
}

type RecipeIntentParam struct {
	FlagName    string
	InputName   string
	GoName      string
	Type        RecipeIntentParamType
	Description string
	Required    bool
	Positional  bool
	Default     string
	BoolDefault bool
	UseEquals   bool
}

type RecipeIntentParamType string

const (
	recipeIntentParamString  RecipeIntentParamType = "string"
	recipeIntentParamNumber  RecipeIntentParamType = "number"
	recipeIntentParamBoolean RecipeIntentParamType = "boolean"
)

func buildRecipeIntents(apiName string, narrative *ReadmeNarrative, reserved map[string]bool) []RecipeIntent {
	if narrative == nil || len(narrative.Recipes) == 0 {
		return nil
	}
	var intents []RecipeIntent
	seen := make(map[string]int)
	for name := range reserved {
		seen[name] = 1
	}
	for _, recipe := range narrative.Recipes {
		intent, ok := recipeIntentFromRecipe(apiName, recipe)
		if !ok {
			continue
		}
		baseName := uniqueMCPToolName(recipe.Title)
		if baseName == "" {
			continue
		}
		name := baseName
		for seen[name] > 0 {
			seen[baseName]++
			name = fmt.Sprintf("%s_%d", baseName, seen[baseName])
		}
		seen[name] = 1
		intent.Name = name
		intents = append(intents, intent)
	}
	return intents
}

func recipeIntentFromRecipe(apiName string, recipe Recipe) (RecipeIntent, bool) {
	tokens, err := shellargs.Split(recipe.Command)
	if err != nil {
		return RecipeIntent{}, false
	}
	if len(tokens) == 0 || containsRecipeShellOperator(tokens) {
		return RecipeIntent{}, false
	}
	tokens = stripRecipeBinary(apiName, tokens)
	if len(tokens) == 0 {
		return RecipeIntent{}, false
	}

	intent := RecipeIntent{
		Description: strings.TrimSpace(recipe.Explanation),
	}
	if intent.Description == "" {
		intent.Description = strings.TrimSpace(recipe.Title)
	}

	nonTrivialInputs := 0
	paramInputNames := map[string]int{}
	paramGoNames := map[string]int{}
	commandWords := 0
	seenFlag := false
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		if !strings.HasPrefix(token, "--") || token == "--" {
			if commandWords > 0 {
				if inputName, ok := recipePositionalInputName(token); ok {
					param := RecipeIntentParam{
						InputName:   uniqueRecipeParamInputName(inputName, paramInputNames),
						GoName:      uniqueRecipeParamGoName(inputName, paramGoNames),
						Type:        recipeIntentParamString,
						Description: "Override the recipe's positional " + inputName + " value.",
						Required:    true,
						Positional:  true,
					}
					intent.Params = append(intent.Params, param)
					intent.Args = append(intent.Args, RecipeIntentArg{Param: param})
					nonTrivialInputs++
					continue
				}
			}
			if seenFlag || commandWords >= 2 {
				return RecipeIntent{}, false
			}
			intent.Command = append(intent.Command, token)
			intent.Args = append(intent.Args, RecipeIntentArg{Static: true, Token: token})
			commandWords++
			continue
		}

		seenFlag = true
		name, value, hasValue := strings.Cut(strings.TrimPrefix(token, "--"), "=")
		if name == "" {
			continue
		}
		useEquals := hasValue
		if recipeFlagIsStatic(name) {
			intent.Command = append(intent.Command, "--"+name)
			intent.Args = append(intent.Args, RecipeIntentArg{Static: true, Token: "--" + name})
			if !hasValue && i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1], "-") {
				value = tokens[i+1]
				hasValue = true
				i++
			}
			if hasValue && value != "" {
				intent.Command = append(intent.Command, value)
				intent.Args = append(intent.Args, RecipeIntentArg{Static: true, Token: value})
			}
			continue
		}
		if !hasValue && i+1 < len(tokens) && isRecipePlaceholder(tokens[i+1]) {
			value = tokens[i+1]
			hasValue = true
			i++
		} else if !hasValue && i+1 < len(tokens) && !strings.HasPrefix(tokens[i+1], "-") {
			return RecipeIntent{}, false
		}

		param := RecipeIntentParam{
			FlagName:    name,
			InputName:   uniqueRecipeParamInputName(name, paramInputNames),
			GoName:      uniqueRecipeParamGoName(name, paramGoNames),
			Type:        recipeIntentParamString,
			Description: "Override the recipe's --" + name + " value.",
			UseEquals:   useEquals,
		}
		if !hasValue {
			param.Type = recipeIntentParamBoolean
			param.BoolDefault = true
			param.Default = "true"
		} else if isRecipePlaceholder(value) {
			param.Required = true
			param.Default = ""
		} else {
			param.Default = value
			param.Type = recipeParamType(value)
		}
		intent.Params = append(intent.Params, param)
		intent.Args = append(intent.Args, RecipeIntentArg{Param: param})
		nonTrivialInputs++
	}
	if len(intent.Command) == 0 || nonTrivialInputs == 0 {
		return RecipeIntent{}, false
	}
	return intent, true
}

func recipeFlagIsStatic(name string) bool {
	return name == "json" || name == "agent"
}

func recipeParamType(value string) RecipeIntentParamType {
	if _, err := strconv.ParseFloat(value, 64); err == nil {
		return recipeIntentParamNumber
	}
	return recipeIntentParamString
}

func reservedMCPToolNames(api *spec.APISpec, vision VisionTemplateSet, novel []NovelFeature) map[string]bool {
	reserved := map[string]bool{
		"context": true,
	}
	if api == nil {
		return reserved
	}
	for _, intent := range api.MCP.Intents {
		reserveMCPName(reserved, intent.Name)
	}
	if api.MCP.IsCodeOrchestration() {
		reserveMCPName(reserved, naming.Snake(api.Name)+"_search")
		reserveMCPName(reserved, naming.Snake(api.Name)+"_execute")
	}
	if vision.Search {
		reserved["search"] = true
	}
	if vision.Store {
		reserved["sql"] = true
	}
	for name := range vision.CmdNames() {
		reserveMCPName(reserved, name)
	}
	for name, resource := range api.Resources {
		for endpointName := range resource.Endpoints {
			reserveMCPName(reserved, fmt.Sprintf("%s_%s", naming.Snake(name), naming.Snake(endpointName)))
		}
		for subName, subResource := range resource.SubResources {
			for endpointName := range subResource.Endpoints {
				reserveMCPName(reserved, fmt.Sprintf("%s_%s_%s", naming.Snake(name), naming.Snake(subName), naming.Snake(endpointName)))
			}
		}
	}
	for _, feature := range novel {
		reserveMCPName(reserved, feature.Command)
	}
	return reserved
}

func reserveMCPName(reserved map[string]bool, name string) {
	if toolName := uniqueMCPToolName(name); toolName != "" {
		reserved[toolName] = true
	}
}

func containsRecipeShellOperator(tokens []string) bool {
	for _, token := range tokens {
		switch token {
		case "|", "||", "&&", ";", ">", ">>", "<", "<<":
			return true
		}
		if strings.Contains(token, "$(") || strings.Contains(token, "`") || containsRecipeShellVariable(token) {
			return true
		}
	}
	return false
}

func containsRecipeShellVariable(token string) bool {
	for i := 0; i < len(token)-1; i++ {
		if token[i] != '$' {
			continue
		}
		next := token[i+1]
		if next == '{' || next == '_' || (next >= 'A' && next <= 'Z') || (next >= 'a' && next <= 'z') {
			return true
		}
	}
	return false
}

func stripRecipeBinary(apiName string, tokens []string) []string {
	if len(tokens) == 0 {
		return tokens
	}
	first := strings.TrimSpace(tokens[0])
	switch {
	case first == "<cli>" || first == apiName || first == apiName+"-pp-cli":
		return tokens[1:]
	case strings.HasSuffix(first, "-pp-cli"):
		return tokens[1:]
	default:
		return tokens
	}
}

func isRecipePlaceholder(token string) bool {
	token = strings.TrimSpace(token)
	return (strings.HasPrefix(token, "<") && strings.HasSuffix(token, ">")) ||
		(strings.HasPrefix(token, "[") && strings.HasSuffix(token, "]"))
}

func recipePositionalInputName(token string) (string, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", false
	}
	if isRecipePlaceholder(token) {
		name := strings.Trim(token, "<>[]")
		if inputName := uniqueMCPToolName(name); inputName != "" {
			return inputName, true
		}
		return "value", true
	}
	if strings.HasPrefix(token, "http://") || strings.HasPrefix(token, "https://") {
		return "url", true
	}
	if looksLikeRecipeVersion(token) {
		return "version", true
	}
	if _, err := strconv.ParseFloat(token, 64); err == nil {
		return "id", true
	}
	if strings.Contains(token, "/") {
		return "path", true
	}
	if looksLikeRecipeDomain(token) {
		return "domain", true
	}
	if strings.Contains(token, "-") || strings.Contains(token, "_") {
		return "slug", true
	}
	return "", false
}

func looksLikeRecipeVersion(token string) bool {
	withoutPrefix := strings.TrimPrefix(strings.TrimPrefix(token, "v"), "V")
	hasVersionPrefix := withoutPrefix != token
	if withoutPrefix == "" || strings.ContainsAny(withoutPrefix, "/:@") {
		return false
	}
	core, _, _ := strings.Cut(withoutPrefix, "-")
	parts := strings.Split(core, ".")
	if len(parts) < 2 {
		return false
	}
	if !hasVersionPrefix && len(parts) < 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

func looksLikeRecipeDomain(token string) bool {
	if strings.ContainsAny(token, "/:@") || strings.HasPrefix(token, ".") || strings.HasSuffix(token, ".") {
		return false
	}
	if looksLikeRecipeVersion(token) {
		return false
	}
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return false
	}
	if slices.Contains(parts, "") {
		return false
	}
	return true
}

func uniqueMCPToolName(title string) string {
	return naming.SnakeIdentifier(title)
}

func uniqueRecipeParamInputName(flagName string, seen map[string]int) string {
	base := uniqueMCPToolName(flagName)
	if base == "" {
		base = "value"
	}
	return uniqueWithNumericSuffix(base, seen)
}

func uniqueRecipeParamGoName(flagName string, seen map[string]int) string {
	base := toPascal(flagName)
	if base == "" {
		base = "Value"
	}
	return uniqueWithNumericSuffix(base, seen)
}

func uniqueWithNumericSuffix(base string, seen map[string]int) string {
	name := base
	for seen[name] > 0 {
		seen[base]++
		name = fmt.Sprintf("%s%d", base, seen[base])
	}
	seen[name] = 1
	return name
}
