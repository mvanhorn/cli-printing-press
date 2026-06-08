package spec

import (
	"fmt"
	"reflect"
	"strings"
)

// GuardStructuralStrings is the data-layer defense against Go-code injection in
// printed CLIs: it walks a parsed APISpec and rejects any *structural* string
// (URLs, names, headers, paths, params, auth fields, env var names, …) that
// contains a character able to break out of a generated Go double-quoted string
// literal — `"`, backtick, backslash, or a control character. Such a value, if
// emitted into generated Go source, could close the literal and inject code; an
// untrusted sniffed/imported spec is the threat model.
//
// It is enforced at the parse chokepoints (spec.ParseBytes and openapi.Parse)
// unconditionally, so it holds even when generation runs with --validate=false.
// The template layer still escapes these values (printf %q / oneline) as
// defense in depth; this guard closes the whole class at one point regardless of
// which template site consumes a field.
//
// Fields that are *prose* (descriptions, instructions, titles, free-text shown
// to humans and emitted only through quote-stripping helpers like oneline) are
// exempt via a `pp:"prose"` struct tag; the exemption propagates to a field's
// whole subtree.
// guardMode classifies how a field is emitted into generated Go, which decides
// which characters can break out of its literal:
//   - modeStructural: emitted into a Go double-quoted string literal (or used as
//     an identifier). Reject " ` \ and control chars. This is the default.
//   - modeRawString (`pp:"rawstring"`): emitted only inside a Go raw string
//     (backtick) — JSON payloads, GraphQL selections, regexps. " \ and newlines
//     are legal there; only a backtick can break out, so reject just backtick.
//   - modeProse (`pp:"prose"`): free text emitted only through quote-stripping
//     helpers (oneline) or escaped via %q at emit time. No restriction.
type guardMode int

const (
	modeStructural guardMode = iota
	modeRawString
	modeProse
)

func GuardStructuralStrings(s *APISpec) error {
	if s == nil {
		return nil
	}
	return walkStructural(reflect.ValueOf(s), "spec", modeStructural)
}

func walkStructural(v reflect.Value, path string, mode guardMode) error {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return walkStructural(v.Elem(), path, mode)
	case reflect.String:
		return checkGuardedString(v.String(), path, mode)
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if err := walkStructural(v.Index(i), fmt.Sprintf("%s[%d]", path, i), mode); err != nil {
				return err
			}
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			// Map keys are structural identifiers (resource/endpoint names,
			// header names, template-var names) regardless of the value mode,
			// unless the whole field is prose.
			if k.Kind() == reflect.String && mode == modeStructural {
				if err := checkGuardedString(k.String(), path+".<key>", modeStructural); err != nil {
					return err
				}
			}
			if err := walkStructural(v.MapIndex(k), fmt.Sprintf("%s[%v]", path, k.Interface()), mode); err != nil {
				return err
			}
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" {
				continue // unexported
			}
			fieldMode := mode
			switch f.Tag.Get("pp") {
			case "prose":
				fieldMode = modeProse
			case "rawstring":
				if mode != modeProse { // prose stays the most permissive
					fieldMode = modeRawString
				}
			}
			if err := walkStructural(v.Field(i), path+"."+f.Name, fieldMode); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkGuardedString(s, path string, mode guardMode) error {
	field := strings.TrimPrefix(path, "spec.")
	switch mode {
	case modeProse:
		return nil
	case modeRawString:
		if strings.ContainsRune(s, '`') {
			return fmt.Errorf(
				"raw-string spec field %s contains a backtick: it would break out of the generated Go raw string literal it is emitted into",
				field)
		}
		return nil
	default: // modeStructural
		for _, r := range s {
			if r == '"' || r == '`' || r == '\\' || r < 0x20 {
				return fmt.Errorf(
					"structural spec field %s contains disallowed character %q: URLs, names, paths, headers, params and other non-prose fields must not contain quotes, backticks, backslashes, or control characters (they could break out of a generated Go string literal)",
					field, r)
			}
		}
		return nil
	}
}
