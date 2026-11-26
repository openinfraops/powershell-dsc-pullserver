package schema

type DscKeyValue struct {
	Key   string `json:"Key"`
	Value string `json:"Value"`
}

type DscReport struct {
	JobId               string        `json:"JobId"`
	OperationType       string        `json:"OperationType,omitempty"`
	RefreshMode         string        `json:"RefreshMode,omitempty"`
	Status              string        `json:"Status,omitempty"`
	ReportFormatVersion string        `json:"ReportFormatVersion"`
	StartTime           string        `json:"StartTime,omitempty"`
	EndTime             string        `json:"EndTime,omitempty"`
	RebootRequested     FlexBool      `json:"RebootRequested"`
	Errors              []string      `json:"Errors"`
	StatusData          []string      `json:"StatusData"`
	AdditionalData      []DscKeyValue `json:"AdditionalData"`
}
