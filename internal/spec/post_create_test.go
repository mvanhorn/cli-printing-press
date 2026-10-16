package spec

import "testing"

func TestCreateShapedPOST(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		endpoint string
		want     bool
	}{
		{name: "collection add", path: "/widgets", endpoint: "add-widget", want: true},
		{name: "collection create", path: "/widgets", endpoint: "create", want: true},
		{name: "bare collection generic name", path: "/widgets", endpoint: "widgets", want: true},
		{name: "bare collection post", path: "/widgets", endpoint: "post", want: true},
		{name: "insert note", path: "/notes", endpoint: "insert-note", want: true},
		{name: "nested collection add", path: "/accounts/{id}/widgets", endpoint: "add", want: true},
		{name: "nested collection generic name", path: "/accounts/{id}/widgets", endpoint: "widgets", want: false},
		{name: "item id post", path: "/items/{id}", endpoint: "post", want: false},
		{name: "item id named create", path: "/items/{id}", endpoint: "create", want: false},
		{name: "submit render", path: "/renders", endpoint: "submit", want: false},
		{name: "submit render kebab", path: "/renders", endpoint: "submit-render", want: false},
		{name: "cancel action", path: "/distributors/{id}/cancel", endpoint: "cancel", want: false},
		{name: "cancel named create", path: "/distributors/{id}/cancel", endpoint: "create", want: false},
		{name: "colon block", path: "/executables/{uuid}:block", endpoint: "block", want: false},
		{name: "refund", path: "/orders/{id}/refund", endpoint: "refund-order", want: false},
		{name: "revoke", path: "/tokens/{id}/revoke", endpoint: "revoke", want: false},
		{name: "upload action name", path: "/files", endpoint: "upload", want: false},
		{name: "plural uploads is not the upload action", path: "/uploads", endpoint: "uploads", want: true},
		{name: "restore", path: "/jobs/{id}/restore", endpoint: "restore", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := CreateShapedPOST(tt.path, tt.endpoint); got != tt.want {
				t.Fatalf("CreateShapedPOST(%q, %q) = %v, want %v", tt.path, tt.endpoint, got, tt.want)
			}
		})
	}
}
