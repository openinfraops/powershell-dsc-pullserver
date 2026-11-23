package schema

type Agent struct {
	AgentId               string              `json:"agent_id"`
	NodeName              string              `json:"node_name"`
	LCMVersion            *string             `json:"lcm_version,omitempty"`
	RegistrationType      *string             `json:"registration_type,omitempty"`
	CertificateThumbprint *string             `json:"certificate_thumbprint,omitempty"`
	CertificateSubject    *string             `json:"certificate_subject,omitempty"`
	CertificateIssuer     *string             `json:"certificate_issuer,omitempty"`
	CertificateNotBefore  *string             `json:"certificate_notbefore,omitempty"`
	CertificateNotAfter   *string             `json:"certificate_notafter,omitempty"`
	RegisteredAt          *string             `json:"registered_at,omitempty"`
	LastCommunication     string              `json:"last_communication"`
	HasErrorLastReport    bool                `json:"has_error_last_report"`
	Configurations        []string            `json:"configurations,omitempty"`
	Tags                  map[string][]string `json:"tags,omitempty"`
	State                 *string             `json:"state,omitempty"`
	NeedsRecompile        bool                `json:"needs_recompile"`
	ForceCertRotation     bool                `json:"force_cert_rotation"`
	DeletedAt             *string             `json:"deleted_at,omitempty"`
	DeletedBy             *string             `json:"deleted_by,omitempty"`
}
