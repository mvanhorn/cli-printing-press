package authdoctor

// Status classifies a single API's auth credential state.
type Status string

const (
	StatusOK         Status = "ok"
	StatusSuspicious Status = "suspicious"
	StatusNotSet     Status = "not_set"
	StatusInfo       Status = "info"
	StatusNoAuth     Status = "no_auth"
	StatusUnknown    Status = "unknown"
)

// Finding is one row of the auth doctor table. Each installed API may
// produce one or more Findings (one per declared env var; one with
// StatusNoAuth when the manifest declares no auth).
type Finding struct {
	API         string `json:"api"`
	Type        string `json:"type"`
	EnvVar      string `json:"env_var,omitempty"`
	Status      Status `json:"status"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

// Summary is the count of findings by status, suitable for the summary
// line of the table renderer and the "summary" field in JSON output.
type Summary struct {
	OK         int `json:"ok"`
	Suspicious int `json:"suspicious"`
	NotSet     int `json:"not_set"`
	Info       int `json:"info"`
	NoAuth     int `json:"no_auth"`
	Unknown    int `json:"unknown"`
}

// Summarize counts findings by status.
func Summarize(findings []Finding) Summary {
	var s Summary
	for _, f := range findings {
		switch f.Status {
		case StatusOK:
			s.OK++
		case StatusSuspicious:
			s.Suspicious++
		case StatusNotSet:
			s.NotSet++
		case StatusInfo:
			s.Info++
		case StatusNoAuth:
			s.NoAuth++
		case StatusUnknown:
			s.Unknown++
		}
	}
	return s
}
