package agentcookiesecret

import "errors"

var ErrInvalidCLIName = errors.New("invalid cli name")

type Source string

const (
	SourceBusPlain  Source = "bus_plain"
	SourceBusSealed Source = "bus_sealed"
)

type DetailedResult struct {
	Env     map[string]string
	Sources map[string]Source
}

func LoadDetailed(string, string) (*DetailedResult, error) {
	return &DetailedResult{
		Env:     map[string]string{},
		Sources: map[string]Source{},
	}, nil
}
