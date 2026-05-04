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
