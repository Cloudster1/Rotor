package config

// The audit log as administrators read it (#36); docs/api-contract-m7-audit.md describes it.
const (
	// AuditPageSize and AuditMaxPage bound one window of the log.
	AuditPageSize = 50
	AuditMaxPage  = 200
	// AuditMaxExport bounds one export; a larger match is refused rather than cut, since a cut record reads as whole.
	AuditMaxExport = 50000
)
