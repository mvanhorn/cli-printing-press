// Deprecated: the module path moved to github.com/mvanhorn/cli-printing-press/v4
// after the v4 release. Install the current binary with:
//   go install github.com/mvanhorn/cli-printing-press/v4/cmd/printing-press@latest
module github.com/mvanhorn/cli-printing-press

go 1.26.1

// Retract every release on the legacy v1 module path. `go install ...@latest`
// against the unversioned path silently resolves here and installs a year-old
// binary - the deprecation notice above plus these retractions make Go warn
// loudly and steer users to the /v4 path.
retract [v1.0.0, v1.3.2]

require (
	github.com/getkin/kin-openapi v0.133.0
	github.com/mark3labs/mcp-go v0.47.0
	github.com/spf13/cobra v1.10.2
	github.com/stretchr/testify v1.11.1
	golang.org/x/sync v0.20.0
	golang.org/x/text v0.35.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/go-openapi/jsonpointer v0.21.0 // indirect
	github.com/go-openapi/swag v0.23.0 // indirect
	github.com/google/jsonschema-go v0.4.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mohae/deepcopy v0.0.0-20170929034955-c48cc78d4826 // indirect
	github.com/oasdiff/yaml v0.0.0-20250309154309-f31be36b4037 // indirect
	github.com/oasdiff/yaml3 v0.0.0-20250309153720-d2182401db90 // indirect
	github.com/perimeterx/marshmallow v1.1.5 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/spf13/cast v1.7.1 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	github.com/woodsbury/decimal128 v1.3.0 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
)
