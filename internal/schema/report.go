package schema

type ReportSummary struct {
	ID              int64  `json:"id"`
	JobId           string `json:"job_id"`
	CreatedAt       string `json:"created_at"`
	Status          string `json:"status"`
	RebootRequested bool   `json:"reboot_requested"`
}
