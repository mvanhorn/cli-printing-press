package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	reasonWriteReadbackNoSibling   = "write read-back skipped: no sibling read"
	reasonWriteReadbackNoID        = "write read-back skipped: id not determined"
	reasonWriteReadbackNoField     = "write read-back skipped: no comparable submitted field"
	reasonWriteReadbackReadFailed  = "write read-back skipped: sibling read failed"
	reasonWriteReadbackNotJSON     = "write read-back skipped: sibling read response is not JSON"
	reasonWriteReadbackAbsent      = "write read-back skipped: submitted field absent from read response"
	reasonWriteReadbackNormalized  = "write read-back skipped: submitted value looks normalized"
	reasonWriteReadbackTransformed = "write read-back skipped: submitted value was transformed"

	// reasonWriteReadbackUnchanged is a format string. The field name is the
	// flag or body key the command submitted.
	reasonWriteReadbackUnchanged = "mutation reported success but read-back shows field %q unchanged"
)

// writeReadbackIgnoredFlags are invocation flags that do not name a resource
// field. Comparing them against a read response would fail correct CLIs.
var writeReadbackIgnoredFlags = map[string]bool{
	"json": true, "dry-run": true, "agent": true, "yes": true, "y": true,
	"confirm": true, "force": true, "execute": true, "apply": true,
	"send": true, "launch": true, "select": true, "compact": true,
	"limit": true, "output": true, "out": true, "format": true, "csv": true,
	"quiet": true, "verbose": true, "timeout": true, "help": true,
	"stdin": true, "no-color": true, "color": true, "config": true,
	"profile": true, "debug": true, "page": true, "per-page": true,
	"cursor": true, "fields": true,
}

// writeReadbackLeafRank is the sibling single-resource reads, best first.
// Collection leaves (list, search) are not reads of the mutated id.
var writeReadbackLeafRank = []string{
	"get", "show", "read", "describe", "view", "info", "lookup", "fetch", "retrieve",
}

// writeReadbackTimestampFields are modification clocks. An advanced clock is
// evidence the write landed when the stored field is not the raw submission.
var writeReadbackTimestampFields = []string{
	"updated_at", "updated_on", "modified_at", "modified_on",
	"last_modified", "modification_time", "updated_time",
}

// liveDogfoodReadbackExec runs one dogfood subprocess. pathLen is the
// command path length so fixture copying does not treat the command words
// as file arguments.
type liveDogfoodReadbackExec func(args []string, pathLen int) liveDogfoodRun

type writeReadbackSession struct {
	commandName string
	kind        string
	skipReason  string
	siblingPath []string
	siblingHelp string
	readArgs    []string
	fields      map[string]string
	fieldNames  map[string]string
	before      any
	exec        liveDogfoodReadbackExec
	authEnv     string
}

// startLiveDogfoodWriteReadback plans a persistence check for a live
// create/update and, for an update, reads the resource before the write.
// It returns nil when this invocation is not a live create/update, including
// dry-run and preview runs, so those commands keep today's matrix rows.
func startLiveDogfoodWriteReadback(command liveDogfoodCommand, ctx resolveCtx, runArgs []string, stdin []byte, preview bool) *writeReadbackSession {
	return startLiveDogfoodWriteReadbackWith(command, ctx, runArgs, stdin, preview, nil)
}

func startLiveDogfoodWriteReadbackWith(command liveDogfoodCommand, ctx resolveCtx, runArgs []string, stdin []byte, preview bool, exec liveDogfoodReadbackExec) *writeReadbackSession {
	if !liveDogfoodWriteReadbackApplies(command, runArgs, preview) {
		return nil
	}
	if exec == nil {
		exec = func(args []string, pathLen int) liveDogfoodRun {
			timeout := ctx.timeout
			if timeout <= 0 {
				timeout = 30 * time.Second
			}
			return runLiveDogfoodMatrixProcess(ctx.binaryPath, ctx.cliDir, args, pathLen, timeout, nil)
		}
	}
	kind := liveDogfoodWriteReadbackKind(command)
	session := &writeReadbackSession{
		commandName: strings.Join(command.Path, " "),
		kind:        kind,
		fields:      map[string]string{},
		fieldNames:  map[string]string{},
		exec:        exec,
	}
	session.fields, session.fieldNames = liveDogfoodSubmittedFields(command, runArgs, stdin)
	sibling := findWriteReadbackSibling(command, ctx.siblings[liveDogfoodParentKey(command)])
	if sibling == nil {
		session.skipReason = reasonWriteReadbackNoSibling
		return session
	}
	session.siblingPath = append([]string{}, sibling.Path...)
	helpRun := exec(append(append([]string{}, sibling.Path...), "--help"), len(sibling.Path))
	if helpRun.exitCode != 0 || helpRun.setupFailure {
		session.skipReason = writeReadbackReadFailedReason(helpRun)
		return session
	}
	session.siblingHelp = helpRun.stdout + helpRun.stderr
	if len(session.fields) == 0 {
		session.skipReason = reasonWriteReadbackNoField
		return session
	}
	if kind == "update" {
		ids := liveDogfoodResourceIDs(command, runArgs)
		args, reason := buildWriteReadbackArgs(session.siblingPath, session.siblingHelp, ids)
		if reason != "" {
			session.skipReason = reason
			return session
		}
		session.readArgs = args
		baseline := exec(args, len(session.siblingPath))
		if baseline.exitCode == 0 && !baseline.setupFailure {
			if decoded, ok := decodeLiveDogfoodJSONValue(baseline.stdout); ok {
				session.before = decoded
			}
		}
	}
	return session
}

func (s *writeReadbackSession) finish(happy LiveDogfoodTestResult, happyRun liveDogfoodRun, authEnv string) *LiveDogfoodTestResult {
	if s == nil || happy.Status != LiveDogfoodStatusPass || happy.requestsDryRun {
		return nil
	}
	s.authEnv = authEnv
	if s.skipReason != "" {
		return s.skipped(s.skipReason, liveDogfoodRun{})
	}
	readArgs := s.readArgs
	if s.kind == "create" {
		decoded, ok := decodeLiveDogfoodJSONValue(happyRun.stdout)
		if !ok {
			return s.skipped(reasonWriteReadbackNoID, liveDogfoodRun{})
		}
		id, ok := extractCreatedID(decoded)
		if !ok {
			return s.skipped(reasonWriteReadbackNoID, liveDogfoodRun{})
		}
		args, reason := buildWriteReadbackArgs(s.siblingPath, s.siblingHelp, []string{id})
		if reason != "" {
			return s.skipped(reason, liveDogfoodRun{})
		}
		readArgs = args
		s.readArgs = args
	}
	afterRun := s.exec(readArgs, len(s.siblingPath))
	if afterRun.exitCode != 0 || afterRun.setupFailure {
		return s.skipped(writeReadbackReadFailedReason(afterRun), afterRun)
	}
	after, ok := decodeLiveDogfoodJSONValue(afterRun.stdout)
	if !ok {
		return s.skipped(reasonWriteReadbackNotJSON, afterRun)
	}
	status, reason := judgeWriteReadback(s.fields, s.fieldNames, s.before, after)
	result := LiveDogfoodTestResult{
		Command:      s.commandName,
		Kind:         LiveDogfoodTestWriteReadback,
		Args:         append([]string{}, readArgs...),
		Status:       status,
		Reason:       reason,
		OutputSample: sampleLiveDogfoodOutput(s.authEnv, afterRun.stdout, afterRun.stderr),
	}
	return &result
}

func (s *writeReadbackSession) skipped(reason string, run liveDogfoodRun) *LiveDogfoodTestResult {
	result := skippedLiveDogfoodResult(s.commandName, LiveDogfoodTestWriteReadback, reason)
	if len(s.readArgs) > 0 {
		result.Args = append([]string{}, s.readArgs...)
	}
	if run.stdout != "" || run.stderr != "" {
		result.OutputSample = sampleLiveDogfoodOutput(s.authEnv, run.stdout, run.stderr)
	}
	return &result
}

func liveDogfoodWriteReadbackApplies(command liveDogfoodCommand, runArgs []string, preview bool) bool {
	if preview || liveDogfoodWriteReadbackKind(command) == "" {
		return false
	}
	if liveDogfoodArgsContainDryRun(runArgs) {
		return false
	}
	return true
}

// liveDogfoodWriteReadbackKind reports "create" or "update" for a command
// whose live success should be re-fetched. Other mutations (delete, send,
// render) are left alone: a missing resource or an action with no body
// field is not evidence the write was ignored.
func liveDogfoodWriteReadbackKind(command liveDogfoodCommand) string {
	if annotationIsTrueValue(command.Annotations[mcpReadOnlyAnnotation]) ||
		annotationIsTrueValue(command.Annotations[mcpLocalWriteAnnotation]) {
		return ""
	}
	method := strings.ToUpper(strings.TrimSpace(command.Annotations[endpointMethodAnnotation]))
	if method == "DELETE" || method == "GET" || method == "HEAD" {
		return ""
	}
	leaf := ""
	if len(command.Path) > 0 {
		leaf = command.Path[len(command.Path)-1]
	}
	tokens := commandNameTokens(leaf)
	update := writeReadbackToken(tokens, "update", "patch", "edit", "set", "modify", "replace", "put")
	create := writeReadbackToken(tokens, "create", "add", "new", "post")
	switch {
	case method == "PUT" || method == "PATCH" || update:
		return "update"
	case create && (method == "POST" || method == ""):
		return "create"
	default:
		return ""
	}
}

func writeReadbackToken(tokens []string, names ...string) bool {
	for _, token := range tokens {
		if slices.Contains(names, token) {
			return true
		}
	}
	return false
}

func liveDogfoodParentKey(command liveDogfoodCommand) string {
	if len(command.Path) == 0 {
		return ""
	}
	return strings.Join(command.Path[:len(command.Path)-1], " ")
}

func findWriteReadbackSibling(command liveDogfoodCommand, siblings []liveDogfoodCommand) *liveDogfoodCommand {
	self := strings.Join(command.Path, " ")
	bestRank := len(writeReadbackLeafRank)
	var best *liveDogfoodCommand
	for i := range siblings {
		candidate := &siblings[i]
		if strings.Join(candidate.Path, " ") == self {
			continue
		}
		if annotationIsTrueValue(candidate.Annotations[liveDogfoodParentGroupAnnotation]) {
			continue
		}
		rank, ok := writeReadbackSiblingRank(candidate)
		if !ok {
			continue
		}
		path := strings.Join(candidate.Path, " ")
		if best == nil || rank < bestRank || (rank == bestRank && path < strings.Join(best.Path, " ")) {
			best = candidate
			bestRank = rank
		}
	}
	return best
}

func writeReadbackSiblingRank(command *liveDogfoodCommand) (int, bool) {
	if command == nil || len(command.Path) == 0 {
		return 0, false
	}
	method := strings.ToUpper(strings.TrimSpace(command.Annotations[endpointMethodAnnotation]))
	readOnly := annotationIsTrueValue(command.Annotations[mcpReadOnlyAnnotation])
	if method != "" && method != "GET" && !readOnly {
		return 0, false
	}
	tokens := commandNameTokens(command.Path[len(command.Path)-1])
	for _, token := range tokens {
		if crossAPIListVerbs[token] || cinemaListVerbs[token] || mutatingVerbs[token] {
			return 0, false
		}
	}
	best := len(writeReadbackLeafRank)
	found := false
	for _, token := range tokens {
		rank := slices.Index(writeReadbackLeafRank, token)
		if rank >= 0 && rank < best {
			best = rank
			found = true
		}
	}
	return best, found
}

func liveDogfoodSubmittedFields(command liveDogfoodCommand, runArgs []string, stdin []byte) (map[string]string, map[string]string) {
	placeholders := extractPositionalPlaceholders(liveDogfoodUsageSuffix(command.Help))
	_, flags := splitLiveDogfoodInvocation(command.Path, runArgs, command.Help)
	fields := map[string]string{}
	names := map[string]string{}
	for name, value := range flags {
		if writeReadbackIgnoredFlags[name] || writeReadbackIdentityFlag(name, placeholders) {
			continue
		}
		canon := canonicalFieldName(name)
		if canon == "" {
			continue
		}
		fields[canon] = value
		names[canon] = name
	}
	stdinFields, stdinNames := submittedFieldsFromJSON(stdin)
	for canon, value := range stdinFields {
		if writeReadbackIdentityFlag(stdinNames[canon], placeholders) {
			continue
		}
		if prev, ok := fields[canon]; ok && prev != value {
			delete(fields, canon)
			delete(names, canon)
			continue
		}
		if _, ok := fields[canon]; ok {
			continue
		}
		fields[canon] = value
		names[canon] = stdinNames[canon]
	}
	return fields, names
}

func writeReadbackIdentityFlag(name string, placeholders []string) bool {
	canon := canonicalFieldName(name)
	if canon == "id" {
		return true
	}
	for _, placeholder := range placeholders {
		if !isIDShapePlaceholderName(strings.ToLower(placeholder)) {
			continue
		}
		if canonicalFieldName(placeholder) == canon {
			return true
		}
	}
	return false
}

func liveDogfoodResourceIDs(command liveDogfoodCommand, runArgs []string) []string {
	positionals, flags := splitLiveDogfoodInvocation(command.Path, runArgs, command.Help)
	if len(positionals) > 0 {
		return positionals
	}
	if id, ok := flags["id"]; ok && id != "" {
		return []string{id}
	}
	placeholders := extractPositionalPlaceholders(liveDogfoodUsageSuffix(command.Help))
	names := make([]string, 0, len(flags))
	for name := range flags {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		value := flags[name]
		if value == "" || !writeReadbackIdentityFlag(name, placeholders) {
			continue
		}
		return []string{value}
	}
	return nil
}

func splitLiveDogfoodInvocation(commandPath, args []string, help string) ([]string, map[string]string) {
	flags := map[string]string{}
	var positionals []string
	valueFlags := liveDogfoodFlagValueNames(help)
	shorthands := liveDogfoodShorthandTypes(help)
	shortLong := liveDogfoodShorthandLongNames(help)
	positionalCount := len(extractPositionalPlaceholders(liveDogfoodUsageSuffix(help)))
	start := min(len(commandPath), len(args))
	afterTerminator := false
	for i := start; i < len(args); i++ {
		arg := args[i]
		if arg == "--" && !afterTerminator {
			afterTerminator = true
			continue
		}
		if !afterTerminator && isLiveDogfoodFlagToken(arg) {
			name, value, consumed := liveDogfoodFlagAssignment(args, start, i, positionalCount, valueFlags, shorthands, shortLong)
			if consumed {
				i++
			}
			if name != "" {
				if _, exists := flags[name]; !exists {
					flags[name] = value
				}
			}
			continue
		}
		positionals = append(positionals, arg)
	}
	return positionals, flags
}

func liveDogfoodFlagAssignment(args []string, start, index, positionalCount int, valueFlags map[string]struct{}, shorthands map[byte]bool, shortLong map[byte]string) (name, value string, consumed bool) {
	arg := args[index]
	if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") {
		return liveDogfoodShorthandAssignment(args, index, shorthands, shortLong)
	}
	raw, ok := strings.CutPrefix(arg, "--")
	if !ok || raw == "" {
		return "", "", false
	}
	flagName, flagValue, hasValue := strings.Cut(raw, "=")
	flagName = strings.ToLower(flagName)
	if flagName == "" {
		return "", "", false
	}
	if hasValue {
		return flagName, flagValue, false
	}
	if _, takesValue := valueFlags[flagName]; takesValue || liveDogfoodFlagHasSeparateValueWithTypes(args, start, index, positionalCount, valueFlags) {
		next := index + 1
		if next < len(args) && !isLiveDogfoodFlagToken(args[next]) {
			return flagName, args[next], true
		}
	}
	return flagName, "true", false
}

func liveDogfoodShorthandAssignment(args []string, index int, shorthands map[byte]bool, shortLong map[byte]string) (name, value string, consumed bool) {
	cluster := strings.TrimPrefix(args[index], "-")
	letter, attached, hasAttached := strings.Cut(cluster, "=")
	if len(letter) != 1 {
		consumesNext, _ := liveDogfoodShorthandClusterValue(cluster, shorthands)
		if consumesNext && index+1 < len(args) && !isLiveDogfoodFlagToken(args[index+1]) {
			return "", "", true
		}
		return "", "", false
	}
	long := shortLong[letter[0]]
	takesValue := shorthands[letter[0]]
	if hasAttached {
		return long, attached, false
	}
	if takesValue && index+1 < len(args) && !isLiveDogfoodFlagToken(args[index+1]) {
		return long, args[index+1], true
	}
	if long == "" {
		return "", "", false
	}
	if takesValue {
		return "", "", false
	}
	return long, "true", false
}

func liveDogfoodShorthandLongNames(help string) map[byte]string {
	names := map[byte]string{}
	for line := range strings.SplitSeq(extractFlagsSection(help), "\n") {
		decl, ok := parseLiveDogfoodFlagDecl(line)
		if ok && decl.shorthand != 0 && decl.long != "" {
			names[decl.shorthand] = decl.long
		}
	}
	return names
}

func submittedFieldsFromJSON(raw []byte) (map[string]string, map[string]string) {
	fields := map[string]string{}
	names := map[string]string{}
	if len(bytes.TrimSpace(raw)) == 0 {
		return fields, names
	}
	decoded, ok := decodeLiveDogfoodJSONValue(string(raw))
	if !ok {
		return fields, names
	}
	obj, ok := decoded.(map[string]any)
	if !ok {
		return fields, names
	}
	collectScalarFields(obj, fields, names)
	if len(fields) > 0 {
		return fields, names
	}
	var nested []map[string]any
	for _, key := range sortedMapKeys(obj) {
		child, ok := obj[key].(map[string]any)
		if !ok || writeReadbackProtocolKey(key, obj[key]) {
			continue
		}
		nested = append(nested, child)
	}
	if len(nested) != 1 {
		return fields, names
	}
	collectScalarFields(nested[0], fields, names)
	return fields, names
}

func collectScalarFields(obj map[string]any, fields, names map[string]string) {
	for _, key := range sortedMapKeys(obj) {
		if writeReadbackProtocolKey(key, obj[key]) {
			continue
		}
		value, ok := scalarJSONString(obj[key])
		if !ok {
			continue
		}
		canon := canonicalFieldName(key)
		if canon == "" {
			continue
		}
		if _, exists := fields[canon]; exists {
			continue
		}
		fields[canon] = value
		names[canon] = key
	}
}

func writeReadbackProtocolKey(key string, value any) bool {
	switch canonicalFieldName(key) {
	case "success", "dryrun", "verifynoop":
		return true
	case "status":
		_, numeric := value.(json.Number)
		return numeric
	default:
		return false
	}
}

func buildWriteReadbackArgs(siblingPath []string, help string, ids []string) ([]string, string) {
	if len(siblingPath) == 0 {
		return nil, reasonWriteReadbackNoSibling
	}
	specs := extractPositionalPlaceholderSpecs(liveDogfoodUsageSuffix(help))
	args := append([]string{}, siblingPath...)
	if len(specs) == 0 {
		id := ""
		if len(ids) > 0 {
			id = ids[len(ids)-1]
		}
		if id == "" || !slices.Contains(extractFlagNames(help), "id") {
			return nil, reasonWriteReadbackNoID
		}
		args = append(args, "--id="+id)
	} else {
		chosen, ok := selectWriteReadbackIDs(ids, specs)
		if !ok {
			return nil, reasonWriteReadbackNoID
		}
		args = append(args, chosen...)
	}
	if commandSupportsJSON(help) {
		args = appendJSONArg(args)
	}
	return args, ""
}

func selectWriteReadbackIDs(ids []string, specs []positionalPlaceholder) ([]string, bool) {
	required := 0
	for _, spec := range specs {
		if !spec.optional {
			required++
		}
	}
	if len(ids) < required || (required == 0 && len(ids) == 0) {
		return nil, false
	}
	use := min(len(specs), len(ids))
	chosen := append([]string{}, ids[len(ids)-use:]...)
	return chosen, true
}

func writeReadbackReadFailedReason(run liveDogfoodRun) string {
	if run.setupFailure && run.err != nil {
		return reasonWriteReadbackReadFailed + ": " + run.err.Error()
	}
	if run.exitCode != 0 {
		return fmt.Sprintf("%s (exit %d)", reasonWriteReadbackReadFailed, run.exitCode)
	}
	return reasonWriteReadbackReadFailed
}

// judgeWriteReadback decides whether the re-fetch shows the write.
// A failure requires a pre-read: every comparable field is still holding
// the value it had before the command, and no submitted field matched or
// looks normalized or transformed. One matching field is enough. A second
// field the server ignored is not a failure, because that is a partial
// write rather than a silent no-op. Without a baseline, a different stored
// value may be normalization, so the check stays skipped.
func judgeWriteReadback(fields, names map[string]string, before, after any) (LiveDogfoodStatus, string) {
	if len(fields) == 0 {
		return LiveDogfoodStatusSkip, reasonWriteReadbackNoField
	}
	anyNormalized := false
	anyTransformed := false
	var unchanged []string
	for canon, submitted := range fields {
		afterValue, afterOK := uniqueFieldValue(after, canon)
		if !afterOK {
			continue
		}
		if submittedValuesMatch(submitted, afterValue) {
			return LiveDogfoodStatusPass, ""
		}
		if submittedValueNormalized(submitted, afterValue) {
			anyNormalized = true
			continue
		}
		if before != nil {
			beforeValue, beforeOK := uniqueFieldValue(before, canon)
			if beforeOK && beforeValue == afterValue {
				unchanged = append(unchanged, displayFieldName(names, canon))
				continue
			}
			if beforeOK && beforeValue != afterValue {
				anyTransformed = true
				continue
			}
		}
		anyTransformed = true
	}
	if len(unchanged) > 0 && !anyNormalized && !anyTransformed {
		if advanced, _ := writeReadbackTimestampAdvanced(before, after); advanced {
			return LiveDogfoodStatusPass, ""
		}
		sort.Strings(unchanged)
		return LiveDogfoodStatusFail, fmt.Sprintf(reasonWriteReadbackUnchanged, unchanged[0])
	}
	switch {
	case anyNormalized:
		return LiveDogfoodStatusSkip, reasonWriteReadbackNormalized
	case anyTransformed:
		return LiveDogfoodStatusSkip, reasonWriteReadbackTransformed
	default:
		return LiveDogfoodStatusSkip, reasonWriteReadbackAbsent
	}
}

func displayFieldName(names map[string]string, canon string) string {
	if name := names[canon]; name != "" {
		return name
	}
	return canon
}

func uniqueFieldValue(root any, field string) (string, bool) {
	var values []string
	collectFieldValues(root, field, &values)
	if len(values) == 0 {
		return "", false
	}
	first := values[0]
	for _, value := range values[1:] {
		if value != first {
			return "", false
		}
	}
	return first, true
}

func collectFieldValues(node any, field string, values *[]string) {
	switch typed := node.(type) {
	case map[string]any:
		for _, key := range sortedMapKeys(typed) {
			if canonicalFieldName(key) == canonicalFieldName(field) {
				if value, ok := scalarJSONString(typed[key]); ok {
					*values = append(*values, value)
				}
			}
			collectFieldValues(typed[key], field, values)
		}
	case []any:
		for _, child := range typed {
			collectFieldValues(child, field, values)
		}
	}
}

func writeReadbackTimestampAdvanced(before, after any) (bool, bool) {
	if before == nil || after == nil {
		return false, false
	}
	advanced := false
	comparable := false
	regressed := false
	for _, field := range writeReadbackTimestampFields {
		beforeValue, beforeOK := uniqueFieldValue(before, field)
		afterValue, afterOK := uniqueFieldValue(after, field)
		if !beforeOK || !afterOK {
			continue
		}
		beforeTime, beforeParsed := parseWriteReadbackTime(beforeValue)
		afterTime, afterParsed := parseWriteReadbackTime(afterValue)
		if !beforeParsed || !afterParsed {
			continue
		}
		comparable = true
		switch {
		case afterTime.After(beforeTime):
			advanced = true
		case beforeTime.After(afterTime):
			regressed = true
		}
	}
	if !comparable || (advanced && regressed) {
		return false, false
	}
	return advanced, true
}

func parseWriteReadbackTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, true
		}
	}
	if number, err := strconv.ParseInt(value, 10, 64); err == nil && len(value) >= 10 {
		return time.Unix(number, 0).UTC(), true
	}
	return time.Time{}, false
}

func submittedValuesMatch(submitted, actual string) bool {
	if submitted == actual {
		return true
	}
	if submittedNumbersMatch(submitted, actual) {
		return true
	}
	return submittedBoolsMatch(submitted, actual)
}

func submittedValueNormalized(submitted, actual string) bool {
	if submittedValuesMatch(submitted, actual) {
		return false
	}
	left := strings.TrimSpace(submitted)
	right := strings.TrimSpace(actual)
	return left == right || strings.EqualFold(left, right)
}

func submittedNumbersMatch(submitted, actual string) bool {
	left, leftErr := strconv.ParseFloat(strings.TrimSpace(submitted), 64)
	right, rightErr := strconv.ParseFloat(strings.TrimSpace(actual), 64)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return left == right
}

func submittedBoolsMatch(submitted, actual string) bool {
	if !writeReadbackBoolWord(submitted) || !writeReadbackBoolWord(actual) {
		return false
	}
	left, leftErr := strconv.ParseBool(strings.TrimSpace(submitted))
	right, rightErr := strconv.ParseBool(strings.TrimSpace(actual))
	return leftErr == nil && rightErr == nil && left == right
}

func writeReadbackBoolWord(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "t", "true", "0", "f", "false":
		return true
	default:
		return false
	}
}

func extractCreatedID(root any) (string, bool) {
	level := []any{root}
	for len(level) > 0 {
		var ids []string
		var next []any
		ambiguous := false
		for _, item := range level {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			switch id, scan := objectID(obj); scan {
			case idFound:
				ids = append(ids, id)
			case idAmbiguous:
				ambiguous = true
			}
			for _, key := range sortedMapKeys(obj) {
				child, ok := obj[key].(map[string]any)
				if ok {
					next = append(next, child)
				}
			}
		}
		if ambiguous {
			return "", false
		}
		switch len(ids) {
		case 0:
		case 1:
			return ids[0], true
		default:
			first := ids[0]
			same := true
			for _, id := range ids[1:] {
				if id != first {
					same = false
					break
				}
			}
			if !same {
				return "", false
			}
			return first, true
		}
		level = next
	}
	return "", false
}

type idScan int

const (
	idNone idScan = iota
	idFound
	idAmbiguous
)

func objectID(obj map[string]any) (string, idScan) {
	found := ""
	seen := false
	for _, key := range sortedMapKeys(obj) {
		if canonicalFieldName(key) != "id" {
			continue
		}
		value, ok := scalarJSONString(obj[key])
		if !ok || value == "" {
			continue
		}
		if seen && value != found {
			return "", idAmbiguous
		}
		found = value
		seen = true
	}
	if !seen {
		return "", idNone
	}
	return found, idFound
}

func decodeLiveDogfoodJSONValue(stdout string) (any, bool) {
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" {
		return nil, false
	}
	if value, ok := decodeOneJSONValue(trimmed); ok {
		return value, true
	}
	for line := range strings.SplitSeq(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if value, ok := decodeOneJSONValue(line); ok {
			return value, true
		}
	}
	return nil, false
}

func decodeOneJSONValue(raw string) (any, bool) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, false
	}
	return value, true
}

func scalarJSONString(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case json.Number:
		return typed.String(), true
	case bool:
		return strconv.FormatBool(typed), true
	case nil:
		return "", true
	default:
		return "", false
	}
}

func canonicalFieldName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if r == '_' || r == '-' || r == ' ' {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

func sortedMapKeys(obj map[string]any) []string {
	keys := make([]string, 0, len(obj))
	for key := range obj {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
