package shellargs

import (
	"reflect"
	"testing"
)

func TestSplit(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{`cli goat brownies`, []string{"cli", "goat", "brownies"}},
		{`cli goat "chicken tikka masala" --limit 5`, []string{"cli", "goat", "chicken tikka masala", "--limit", "5"}},
		{`cli  multiple   spaces`, []string{"cli", "multiple", "spaces"}},
		{`cli query \"literal\"`, []string{"cli", "query", `"literal"`}},
		{"cli slots find \\\n  --event-type-id 123 \\\n  --start \"2026-01-01T00:00:00Z\"", []string{"cli", "slots", "find", "--event-type-id", "123", "--start", "2026-01-01T00:00:00Z"}},
		{"cli slots find \\\r\n  --event-type-id 123", []string{"cli", "slots", "find", "--event-type-id", "123"}},
		{"cli --name foo\\\nbar", []string{"cli", "--name", "foobar"}},
		{"cli --name \"foo\\\nbar\"", []string{"cli", "--name", "foobar"}},
		{`cli regex \\d+\\s+goat`, []string{"cli", "regex", `\d+\s+goat`}},
		// Shell line comments: '#' at the start of a word drops the rest of
		// the input. Cobra Example fields routinely use trailing comments.
		{`cli sync                       # full schema + records refresh`, []string{"cli", "sync"}},
		{`cli # whole-line comment`, []string{"cli"}},
		{`cli foo --bar baz  # explanation`, []string{"cli", "foo", "--bar", "baz"}},
		// Quoted '#' is part of the value, not a comment.
		{`cli query "# not a comment"`, []string{"cli", "query", "# not a comment"}},
		// '#' embedded inside an unquoted token (no leading whitespace) is a
		// literal character — bash semantics treat '#' as a comment only at
		// the start of a word.
		{`cli regex foo#bar`, []string{"cli", "regex", "foo#bar"}},
		// Escaped '#' is a literal.
		{`cli \#literal`, []string{"cli", "#literal"}},
	}
	for _, tc := range cases {
		got, err := Split(tc.in)
		if err != nil {
			t.Fatalf("Split(%q): %v", tc.in, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("Split(%q) = %#v, want %#v", tc.in, got, tc.want)
		}
	}
}

func TestSplitUnclosedQuote(t *testing.T) {
	if _, err := Split(`cli "unclosed`); err == nil {
		t.Fatal("expected unclosed quote error")
	}
}

func TestArgsAfterBinary(t *testing.T) {
	got, err := ArgsAfterBinary(`cli goat "chicken tikka masala"`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"goat", "chicken tikka masala"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ArgsAfterBinary() = %#v, want %#v", got, want)
	}

	if _, err := ArgsAfterBinary("cli"); err == nil {
		t.Fatal("expected missing subcommand error")
	}
}
