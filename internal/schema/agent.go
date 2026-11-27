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
	CertificatePublicKey  *string             `json:"certificate_public_key,omitempty"`
	CertificateKeyAlg     *string             `json:"certificate_key_algorithm,omitempty"`
	RegisteredAt          *string             `json:"registered_at,omitempty"`
	LastCommunication     string              `json:"last_communication"`
	HasErrorLastReport    bool                `json:"has_error_last_report"`
	Configurations        []string            `json:"configurations,omitempty"`
	Tags                  map[string][]string `json:"tags,omitempty"`
	State                 *string             `json:"state,omitempty"`
	NeedsRecompile        bool                `json:"needs_recompile"`
	ForceCertRotation     bool                `json:"force_cert_rotation"`
	// RebootPending indique si le nœud a demandé un redémarrage lors de son
	// dernier rapport (dérivé de reports.reboot_requested).
	RebootPending bool `json:"reboot_pending"`
	// RebootPendingSince est l'horodatage du premier rapport de la série courante
	// de demandes de redémarrage (nil si jamais en attente).
	RebootPendingSince *string `json:"reboot_pending_since,omitempty"`
	// RebootRequestedBy identifie la ressource qui a demandé le redémarrage
	// (ModuleName|SourceInfo:ResourceId), nil si inconnue.
	RebootRequestedBy *string `json:"reboot_requested_by,omitempty"`
	// RebootLoop est vrai quand le nœud a demandé un redémarrage sur N rapports
	// consécutifs (seuil configurable).
	RebootLoop bool `json:"reboot_loop"`
	// RebootRequestCount est le nombre de rapports consécutifs avec une demande
	// de redémarrage.
	RebootRequestCount int     `json:"reboot_request_count"`
	DeletedAt          *string `json:"deleted_at,omitempty"`
	DeletedBy          *string `json:"deleted_by,omitempty"`
	// Persistent indique un nœud persistant soumis à la politique de
	// ré-enregistrement (#87). Un tel nœud ne peut pas être ré-enregistré
	// sans respecter le mode configuré (deny/approval/proof/window/allow).
	Persistent bool `json:"persistent"`
	// ReregistrationWindowUntil est la borne limite (UTC) d'une fenêtre de
	// ré-enregistrement ouverte par un admin (mode window de #87).
	ReregistrationWindowUntil *string `json:"reregistration_window_until,omitempty"`
}
