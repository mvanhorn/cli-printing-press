package browsersniff

type HAR struct {
	Log HARLog `json:"log"`
}

type HARLog struct {
	Entries []HAREntry `json:"entries"`
}

type HAREntry struct {
	StartedDateTime string      `json:"startedDateTime,omitempty"`
	Time            float64     `json:"time,omitempty"`
	Request         HARRequest  `json:"request"`
	Response        HARResponse `json:"response"`
}

type HARRequest struct {
	Method   string       `json:"method"`
	URL      string       `json:"url"`
	Headers  []HARHeader  `json:"headers"`
	PostData *HARPostData `json:"postData,omitempty"`
}

type HARPostData struct {
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}

type HARResponse struct {
	Status  int                `json:"status"`
	Headers []HARHeader        `json:"headers,omitempty"`
	Content HARResponseContent `json:"content"`
}

type HARResponseContent struct {
	MimeType string `json:"mimeType"`
	Size     int    `json:"size"`
	Text     string `json:"text"`
}

type HARHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type EnrichedCapture struct {
	TargetURL         string          `json:"target_url"`
	CapturedAt        string          `json:"captured_at"`
	InteractionRounds int             `json:"interaction_rounds"`
	Auth              *AuthCapture    `json:"auth,omitempty"`
	Entries           []EnrichedEntry `json:"entries"`
}

type AuthCapture struct {
	Headers     map[string]string `json:"headers"`
	Cookies     []string          `json:"cookies"`
	Type        string            `json:"type"`   // bearer, api_key, cookie, composed
	Format      string            `json:"format"` // for composed: header template with {cookieName} placeholders
	BoundDomain string            `json:"bound_domain"`
	ExpiresAt   string            `json:"expires_at"`
}

type EnrichedEntry struct {
	Method              string            `json:"method"`
	URL                 string            `json:"url"`
	StartedDateTime     string            `json:"started_date_time,omitempty"`
	DurationMS          float64           `json:"duration_ms,omitempty"`
	RequestBody         string            `json:"request_body"`
	ResponseBody        string            `json:"response_body"`
	ResponseStatus      int               `json:"response_status"`
	ResponseContentType string            `json:"response_content_type"`
	RequestHeaders      map[string]string `json:"request_headers"`
	ResponseHeaders     map[string]string `json:"response_headers,omitempty"`
	Classification      string            `json:"classification"`
	IsNoise             bool              `json:"is_noise"`
}
