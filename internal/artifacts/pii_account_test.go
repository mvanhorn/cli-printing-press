package artifacts

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindPII_AccountID_PositiveShapes(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "json quoted digits and mixed",
			body: `{
  "accountId": "4839201756",
  "partyId": "77120934581",
  "digitalAccountIdentifier": "AB12CD34EF56GH78",
  "account": {"number": "000123456789"},
  "email": "jane.doe@gmail.com"
}
`,
			want: []string{"4839201756", "77120934581", "AB12CD34EF56GH78", "000123456789"},
		},
		{
			name: "json unquoted number",
			body: `{"accountId": 4839201756}` + "\n",
			want: []string{"4839201756"},
		},
		{
			name: "json quoted number string",
			body: `{"AccountID": "4839201756"}` + "\n",
			want: []string{"4839201756"},
		},
		{
			name: "json snake and kebab",
			body: `{"ACCOUNT_ID": "4839201756", "account-id": "77120934581"}` + "\n",
			want: []string{"4839201756", "77120934581"},
		},
		{
			name: "dotted key",
			body: `{"account.number": "000123456789"}` + "\n",
			want: []string{"000123456789"},
		},
		{
			name: "enterprise customer identifier",
			body: `{"enterpriseCustomerIdentifier": "AB12CD34EF56GH78"}` + "\n",
			want: []string{"AB12CD34EF56GH78"},
		},
		{
			name: "customer number",
			body: `{"customer_number": "000123456789", "customerNo": "77120934581"}` + "\n",
			want: []string{"000123456789", "77120934581"},
		},
		{
			name: "yaml quoted single and unquoted",
			body: "accountId: \"4839201756\"\npartyId: '77120934581'\ncustomerNumber: 000123456789\n",
			want: []string{"4839201756", "77120934581", "000123456789"},
		},
		{
			name: "yaml nested number",
			body: "account:\n  number: \"000123456789\"\n",
			want: []string{"000123456789"},
		},
		{
			name: "yaml flow map",
			body: "account: {number: \"000123456789\"}\n",
			want: []string{"000123456789"},
		},
		{
			name: "yaml value on a later line",
			body: "accountId:\n\n  \"4839201756\"\n",
			want: []string{"4839201756"},
		},
		{
			name: "json value two lines later",
			body: "{\n  \"accountId\":\n\n    \"4839201756\"\n}\n",
			want: []string{"4839201756"},
		},
		{
			name: "json nested object split across lines",
			body: "{\n  \"account\":\n  {\n    \"number\": \"000123456789\"\n  }\n}\n",
			want: []string{"000123456789"},
		},
		{
			name: "json embedded in yaml example",
			body: "name: sniffed-bank\nresources:\n  summary:\n    example: {\"email\": \"jane.doe@gmail.com\", \"accountId\": \"4839201756\"}\n",
			want: []string{"4839201756"},
		},
		{
			name: "array element",
			body: `{"accountId": ["4839201756", "12"]}` + "\n",
			want: []string{"4839201756"},
		},
		{
			name: "iban and uuid",
			body: `{"accountNumber": "GB82WEST12345698765432", "partyId": "6ba7b810-9dad-11d1-80b4-00c04fd430c8"}` + "\n",
			want: []string{"GB82WEST12345698765432", "6ba7b810-9dad-11d1-80b4-00c04fd430c8"},
		},
		{
			name: "same value under two keys",
			body: `{"accountId": "4839201756", "partyId": "4839201756"}` + "\n",
			want: []string{"4839201756", "4839201756"},
		},
		{
			name: "crlf json",
			body: "{\"accountId\": \"4839201756\"}\r\n",
			want: []string{"4839201756"},
		},
		{
			name: "leading plus",
			body: `{"accountId": "+4839201756"}` + "\n",
			want: []string{"+4839201756"},
		},
		{
			name: "nested under user and spaced key",
			body: `{"user": {"accountId": "4839201756"}, "account number": "000123456789"}` + "\n",
			want: []string{"4839201756", "000123456789"},
		},
		{
			name: "bom before object",
			body: "\uFEFF{\"partyId\": \"77120934581\"}\n",
			want: []string{"77120934581"},
		},
		{
			name: "garbage prefix then object",
			body: "note {\"accountId\": \"4839201756\"}\n",
			want: []string{"4839201756"},
		},
		{
			name: "unclosed object still flags",
			body: "{\"accountId\": \"4839201756\"\n",
			want: []string{"4839201756"},
		},
		{
			name: "yaml comment and tab nest",
			body: "accountId: \"4839201756\" # captured\naccount:\n\tnumber: \"000123456789\"\n",
			want: []string{"4839201756", "000123456789"},
		},
		{
			name: "commented yaml key",
			body: "# accountId: 4839201756\n",
			want: []string{"4839201756"},
		},
		{
			name: "json escaped slash before account",
			body: `{"url":"https:\/\/bank.invalid","accountId":"AB12CD34EF56GH78"}` + "\n",
			want: []string{"AB12CD34EF56GH78"},
		},
		{
			name: "yaml flow bare scalar",
			body: "account: {number: AB12CD34EF56GH78}\n",
			want: []string{"AB12CD34EF56GH78"},
		},
		{
			name: "yaml flow array",
			body: "accountId: [\"AB12CD34EF56GH78\"]\naccountId: [77120934581]\n",
			want: []string{"AB12CD34EF56GH78", "77120934581"},
		},
		{
			name: "yaml next document keeps its own key",
			body: "account:\n  name: checking\n---\naccountId: AB12CD34EF56GH78\n",
			want: []string{"AB12CD34EF56GH78"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := accountSpans(t, tt.body)
			assert.ElementsMatch(t, tt.want, got)
		})
	}
}

func TestFindPII_AccountID_NegativeShapes(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "short", body: `{"accountId": "123"}` + "\n"},
		{name: "seven digits", body: `{"accountId": "4839201"}` + "\n"},
		{name: "eighteen digits", body: `{"accountId": "123456789012345678"}` + "\n"},
		{name: "short mixed", body: `{"accountId": "AB12CD3"}` + "\n"},
		{name: "sequential", body: `{"accountId": "12345678"}` + "\n"},
		{name: "sequential longer", body: `{"partyId": "1234567890"}` + "\n"},
		{name: "descending", body: `{"accountId": "987654321"}` + "\n"},
		{name: "all zeros", body: `{"accountNumber": "00000000"}` + "\n"},
		{name: "all same", body: `{"accountId": "11111111"}` + "\n"},
		{name: "doc token underscore", body: `{"account_id": "acct_123"}` + "\n"},
		{name: "doc token hyphen", body: `{"accountId": "acct-12345678"}` + "\n"},
		{name: "placeholder word", body: `{"accountId": "example"}` + "\n"},
		{name: "angle placeholder", body: `{"accountId": "<accountId>"}` + "\n"},
		{name: "brace placeholder", body: `{"partyId": "{partyId}"}` + "\n"},
		{name: "replacement sentinel", body: `{"accountId": "PII_ACCOUNT_EXAMPLE"}` + "\n"},
		{name: "rfc uuid", body: `{"accountId": "550e8400-e29b-41d4-a716-446655440000"}` + "\n"},
		{name: "nil uuid", body: `{"accountId": "00000000-0000-0000-0000-000000000000"}` + "\n"},
		{name: "pagination and generic ids", body: `{"page": 4839201756, "id": "77120934581", "offset": 12, "limit": 100, "userId": "4839201756", "request_id": "AB12CD34EF56GH78"}` + "\n"},
		{name: "accounting id is not an account id", body: `{"accountingId": "4839201756", "accountName": "4839201756", "accountsId": "77120934581"}` + "\n"},
		{name: "sibling id after closed account object", body: "{\"account\": {\"name\": \"checking\"}, \"id\": \"4839201756\"}\n"},
		{name: "history id under account", body: `{"account": {"history": {"id": "4839201756"}}}` + "\n"},
		{name: "null and bool", body: `{"accountId": null, "partyId": false}` + "\n"},
		{name: "zip under account key", body: `{"accountId": "12345-6789"}` + "\n"},
		{name: "whitespace prose", body: "accountId: 4839201756 is the sample\n"},
		{name: "generated go is out of scope", body: "package p\nconst x = `{\"accountId\": \"4839201756\"}`\n"},
		{name: "yaml document boundary drops parent", body: "account:\n  name: checking\n---\n- id: AB12CD34EF56GH78\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name := "sample.json"
			if tt.name == "generated go is out of scope" {
				name = "sample.go"
			}
			root := t.TempDir()
			write(t, filepath.Join(root, name), tt.body)
			findings, err := FindPII(root)
			require.NoError(t, err)
			assert.Empty(t, spansOfKind(findings, PIIKindAccountID))
		})
	}
}

func TestFindPII_AccountID_CrossLineNumber(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "sample.json"), "{\n  \"accountId\":\n\n    \"4839201756\"\n}\n")
	findings, err := FindPII(root)
	require.NoError(t, err)
	var hit *PIIFinding
	for i := range findings {
		if findings[i].Kind == PIIKindAccountID {
			hit = &findings[i]
			break
		}
	}
	require.NotNil(t, hit)
	assert.Equal(t, 4, hit.Line)
	assert.Equal(t, "4839201756", hit.MatchedSpan)
	assert.Greater(t, hit.Column, 1)
}

func TestFindPII_AccountID_SingleHitNotDoubled(t *testing.T) {
	got := accountSpans(t, "{\"accountId\": \"4839201756\"}\n")
	assert.Equal(t, []string{"4839201756"}, got)
}

func TestFindPII_AccountID_ExcludedFilesAndOrdinaryIDs(t *testing.T) {
	root := t.TempDir()
	body := `{"accountId": "4839201756"}` + "\n"
	write(t, filepath.Join(root, "tools-manifest.json"), body)
	write(t, filepath.Join(root, PIILedgerFilename), body)
	write(t, filepath.Join(root, "data.json"), `{"page": 1, "id": "42"}`+"\n")
	findings, err := FindPII(root)
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestPIIFindingID_EmailKindUnchanged(t *testing.T) {
	f := PIIFinding{Kind: PIIKindEmail, File: "a.json", Line: 5, MatchedSpan: "alice@example.com"}
	assert.Equal(t, "e44771cb051a", PIIFindingID(f))
}

func TestFindPII_SniffedRootSpecIsScanned(t *testing.T) {
	body := "name: sniffed-bank\nresources:\n  summary:\n    example: {\"email\": \"jane.doe@gmail.com\", \"accountId\": \"4839201756\"}\n"
	manifests := []struct {
		name string
		body string
	}{
		{name: "top-level", body: "{\"spec_source\": \"sniffed\"}\n"},
		{name: "spaced value", body: "{\"spec_source\": \" sniffed \"}\n"},
		{name: "camel key nested", body: "{\"meta\": {\"specSource\": \"Sniffed\"}}\n"},
		{name: "hyphen key", body: "{\"spec-source\": \"SNIFFED\"}\n"},
		{name: "array element", body: "[{\"spec_source\": \"sniffed\"}]\n"},
	}
	for _, tt := range manifests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			write(t, filepath.Join(root, "spec.yaml"), body)
			write(t, filepath.Join(root, printingPressManifestFilename), tt.body)
			findings, err := FindPII(root)
			require.NoError(t, err)
			assert.Contains(t, spansOfKind(findings, PIIKindAccountID), "4839201756")
			assert.NotEmpty(t, spansOfKind(findings, PIIKindEmail))
		})
	}
}

func TestFindPII_RootSpecSourceOnTheSpecItself(t *testing.T) {
	tests := []struct {
		name string
		file string
		body string
		want bool
	}{
		{
			name: "yaml column zero quoted",
			file: "spec.yaml",
			body: "name: bank\nspec_source: \"sniffed\"\naccountId: \"4839201756\"\nemail: jane.doe@gmail.com\n",
			want: true,
		},
		{
			name: "yaml single quoted with comment",
			file: "spec.yml",
			body: "spec_source: 'sniffed' # session capture\naccountId: \"4839201756\"\n",
			want: true,
		},
		{
			name: "yaml SpecSource casing",
			file: "spec.yaml",
			body: "SpecSource: SNIFFED\npartyId: \"77120934581\"\n",
			want: true,
		},
		{
			name: "yaml spaced key and crlf",
			file: "spec.yaml",
			body: "spec source : sniffed\r\naccountId: \"4839201756\"\r\n",
			want: true,
		},
		{
			name: "yaml after document marker",
			file: "spec.yaml",
			body: "---\nspec_source: sniffed\naccountId: \"4839201756\"\n",
			want: true,
		},
		{
			name: "bom then column-zero key",
			file: "spec.yaml",
			body: "\uFEFFspec_source: sniffed\naccountId: \"4839201756\"\n",
			want: true,
		},
		{
			name: "json top-level not first key",
			file: "spec.json",
			body: "{\n  \"name\": \"bank\",\n  \"spec_source\": \"sniffed\",\n  \"accountId\": \"4839201756\"\n}\n",
			want: true,
		},
		{
			name: "indented yaml does not count",
			file: "spec.yaml",
			body: "info:\n  spec_source: sniffed\nemail: jane.doe@gmail.com\n",
			want: false,
		},
		{
			name: "official yaml stays exempt",
			file: "spec.yaml",
			body: "spec_source: official\nemail: jane.doe@gmail.com\naccountId: \"4839201756\"\n",
			want: false,
		},
		{
			name: "json nested example does not count",
			file: "spec.json",
			body: "{\"openapi\": \"3.0.3\", \"paths\": {\"/x\": {\"get\": {\"spec_source\": \"sniffed\", \"example\": {\"email\": \"jane.doe@gmail.com\", \"accountId\": \"4839201756\"}}}}}\n",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			write(t, filepath.Join(root, tt.file), tt.body)
			findings, err := FindPII(root)
			require.NoError(t, err)
			if tt.want {
				assert.NotEmpty(t, spansOfKind(findings, PIIKindAccountID), "sniffed root spec must be scanned")
			} else {
				assert.Empty(t, findings)
			}
		})
	}
}

func TestFindPII_VendorRootSpecStaysExempt(t *testing.T) {
	openapi := "openapi: 3.0.0\ninfo:\n  title: Bank\npaths:\n  /accounts:\n    get:\n      responses:\n        '200':\n          content:\n            application/json:\n              example:\n                email: jane.doe@gmail.com\n                accountId: \"4839201756\"\n"
	for _, source := range []string{"official", "docs", "community", ""} {
		t.Run(source, func(t *testing.T) {
			root := t.TempDir()
			write(t, filepath.Join(root, "spec.yaml"), openapi)
			if source != "" {
				write(t, filepath.Join(root, printingPressManifestFilename), "{\"spec_source\": \""+source+"\"}\n")
			}
			findings, err := FindPII(root)
			require.NoError(t, err)
			assert.Empty(t, findings)
		})
	}
}

func TestFindPII_OfficialManifestDoesNotHideSniffedSpec(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "spec.yaml"), "spec_source: sniffed\naccountId: \"4839201756\"\n")
	write(t, filepath.Join(root, printingPressManifestFilename), "{\"spec_source\": \"official\"}\n")
	findings, err := FindPII(root)
	require.NoError(t, err)
	assert.Contains(t, spansOfKind(findings, PIIKindAccountID), "4839201756")
}

func TestFindPII_SniffedOpenAPIRootSpecIsScanned(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "spec.yaml"), "openapi: 3.0.0\ninfo:\n  title: Bank\npaths: {}\naccountId: \"4839201756\"\n")
	write(t, filepath.Join(root, printingPressManifestFilename), "{\"spec_source\": \"sniffed\"}\n")
	findings, err := FindPII(root)
	require.NoError(t, err)
	assert.Contains(t, spansOfKind(findings, PIIKindAccountID), "4839201756")
}

func TestFindPII_ManifestSignalsThatDoNotSniff(t *testing.T) {
	spec := "email: jane.doe@gmail.com\naccountId: \"4839201756\"\n"
	manifests := []string{
		"{\"description\": \"spec_source: sniffed\"}\n",
		"{\"spec_source_material\": \"sniffed\"}\n",
		"{\"spec_source\": \"browser-sniffed\"}\n",
		"{\"spec_source\": true}\n",
		"not json\n",
		"",
	}
	for _, manifest := range manifests {
		t.Run(manifest, func(t *testing.T) {
			root := t.TempDir()
			write(t, filepath.Join(root, "spec.yaml"), spec)
			if manifest != "" {
				write(t, filepath.Join(root, printingPressManifestFilename), manifest)
			}
			findings, err := FindPII(root)
			require.NoError(t, err)
			assert.Empty(t, findings)
		})
	}
}

func TestFindPII_ManuscriptsVendorSpecStaysExemptWhenSniffed(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, printingPressManifestFilename), "{\"spec_source\": \"sniffed\"}\n")
	write(t, filepath.Join(root, ".manuscripts", "run1", "research", "vendor.yaml"), ""+
		"openapi: 3.0.0\ninfo:\n  title: Vendor\npaths: {}\nemail: jane.doe@gmail.com\naccountId: \"4839201756\"\n")
	findings, err := FindPII(root)
	require.NoError(t, err)
	assert.Empty(t, findings)
}

func TestSplitAccountKey(t *testing.T) {
	assert.Equal(t, []string{"digital", "account", "identifier"}, splitAccountKey("digitalAccountIdentifier"))
	assert.Equal(t, []string{"enterprise", "customer", "identifier"}, splitAccountKey("enterpriseCustomerIdentifier"))
	assert.Equal(t, []string{"account", "id"}, splitAccountKey("ACCOUNT_ID"))
	assert.Equal(t, []string{"account", "number"}, splitAccountKey("account.number"))
	assert.Equal(t, []string{"accounting", "id"}, splitAccountKey("accountingId"))
	assert.False(t, isAccountIdentifierKeyPath([]string{"accountingId"}))
	assert.False(t, isAccountIdentifierKeyPath([]string{"account", "history", "id"}))
	assert.True(t, isAccountIdentifierKeyPath([]string{"account", "number"}))
	assert.True(t, isAccountIdentifierKeyPath([]string{"enterpriseCustomerIdentifier"}))
}

func accountSpans(t *testing.T, body string) []string {
	t.Helper()
	root := t.TempDir()
	write(t, filepath.Join(root, "sample.json"), body)
	findings, err := FindPII(root)
	require.NoError(t, err)
	return spansOfKind(findings, PIIKindAccountID)
}

func spansOfKind(findings []PIIFinding, kind string) []string {
	var out []string
	for _, f := range findings {
		if f.Kind == kind {
			out = append(out, f.MatchedSpan)
		}
	}
	return out
}
