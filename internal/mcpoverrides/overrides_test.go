package mcpoverrides

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

func TestLoad_FileAbsentReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	o, err := Load(dir)
	if err != nil {
		t.Fatalf("expected nil error when file absent, got %v", err)
	}
	if len(o.Descriptions) != 0 {
		t.Fatalf("expected empty Descriptions, got %v", o.Descriptions)
	}
}

func TestLoad_MalformedJSONReturnsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, Filename), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestLoad_RoundtripsDescriptions(t *testing.T) {
	dir := t.TempDir()
	want := `{"descriptions":{"tags_create":"Create a new tag","tags_update":"Update existing tag"}}`
	if err := os.WriteFile(filepath.Join(dir, Filename), []byte(want), 0o644); err != nil {
		t.Fatal(err)
	}
	o, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := o.Descriptions["tags_create"]; got != "Create a new tag" {
		t.Errorf("tags_create = %q, want %q", got, "Create a new tag")
	}
	if got := o.Descriptions["tags_update"]; got != "Update existing tag" {
		t.Errorf("tags_update = %q, want %q", got, "Update existing tag")
	}
}

func TestApply_TopLevelEndpoint(t *testing.T) {
	parsed := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"tags": {
				Endpoints: map[string]spec.Endpoint{
					"create": {Description: "Create a tag"},
				},
			},
		},
	}
	o := Overrides{Descriptions: map[string]string{
		"tags_create": "Create a new tag with required name and optional color.",
	}}
	unmatched := o.Apply(parsed)
	if len(unmatched) != 0 {
		t.Errorf("expected zero unmatched, got %v", unmatched)
	}
	got := parsed.Resources["tags"].Endpoints["create"].Description
	want := "Create a new tag with required name and optional color."
	if got != want {
		t.Errorf("description = %q, want %q", got, want)
	}
}

func TestApply_SubResourceEndpoint(t *testing.T) {
	parsed := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"bookings": {
				SubResources: map[string]spec.Resource{
					"attendees": {
						Endpoints: map[string]spec.Endpoint{
							"add": {Description: "Add an attendee"},
						},
					},
				},
			},
		},
	}
	o := Overrides{Descriptions: map[string]string{
		"bookings_attendees_add": "Add an attendee to a booking by email or userId.",
	}}
	unmatched := o.Apply(parsed)
	if len(unmatched) != 0 {
		t.Errorf("expected zero unmatched, got %v", unmatched)
	}
	got := parsed.Resources["bookings"].SubResources["attendees"].Endpoints["add"].Description
	want := "Add an attendee to a booking by email or userId."
	if got != want {
		t.Errorf("description = %q, want %q", got, want)
	}
}

func TestApply_UnmatchedKeysReturned(t *testing.T) {
	parsed := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"tags": {
				Endpoints: map[string]spec.Endpoint{
					"create": {Description: "Create a tag"},
				},
			},
		},
	}
	o := Overrides{Descriptions: map[string]string{
		"tags_create":  "OK",
		"tags_destroy": "TYPO — does not exist",
		"links_lint":   "STALE — endpoint removed",
	}}
	unmatched := o.Apply(parsed)
	sort.Strings(unmatched)
	want := []string{"links_lint", "tags_destroy"}
	if len(unmatched) != len(want) {
		t.Fatalf("unmatched len = %d (%v), want %v", len(unmatched), unmatched, want)
	}
	for i := range want {
		if unmatched[i] != want[i] {
			t.Errorf("unmatched[%d] = %q, want %q", i, unmatched[i], want[i])
		}
	}
}

func TestApply_EmptyOverridesIsNoOp(t *testing.T) {
	parsed := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"tags": {
				Endpoints: map[string]spec.Endpoint{
					"create": {Description: "Create a tag"},
				},
			},
		},
	}
	o := Overrides{}
	unmatched := o.Apply(parsed)
	if unmatched != nil {
		t.Errorf("expected nil unmatched for empty overrides, got %v", unmatched)
	}
	if got := parsed.Resources["tags"].Endpoints["create"].Description; got != "Create a tag" {
		t.Errorf("description mutated unexpectedly to %q", got)
	}
}

func TestApply_NilParsedIsNoOp(t *testing.T) {
	o := Overrides{Descriptions: map[string]string{"x": "y"}}
	if got := o.Apply(nil); got != nil {
		t.Errorf("expected nil unmatched for nil parsed, got %v", got)
	}
}

func TestToolName(t *testing.T) {
	tests := []struct {
		resource, sub, endpoint, want string
	}{
		{"tags", "", "create", "tags_create"},
		{"bookings", "attendees", "add", "bookings_attendees_add"},
		{"event-types", "", "list", "event-types_list"},
	}
	for _, tt := range tests {
		got := ToolName(tt.resource, tt.sub, tt.endpoint)
		if got != tt.want {
			t.Errorf("ToolName(%q, %q, %q) = %q, want %q", tt.resource, tt.sub, tt.endpoint, got, tt.want)
		}
	}
}
