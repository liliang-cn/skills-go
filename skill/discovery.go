package skill

// SkillScope identifies where a skill was discovered from.
type SkillScope string

const (
	SkillScopeProject SkillScope = "project"
	SkillScopeUser    SkillScope = "user"
	SkillScopeCustom  SkillScope = "custom"
)

// DiagnosticSeverity identifies the severity of a discovery diagnostic.
type DiagnosticSeverity string

const (
	DiagnosticSeverityInfo    DiagnosticSeverity = "info"
	DiagnosticSeverityWarning DiagnosticSeverity = "warning"
)

// Diagnostic records non-fatal discovery issues such as trust gating and name collisions.
type Diagnostic struct {
	Severity   DiagnosticSeverity `json:"severity"`
	Code       string             `json:"code"`
	Message    string             `json:"message"`
	SkillName  string             `json:"skill_name,omitempty"`
	Path       string             `json:"path,omitempty"`
	Scope      SkillScope         `json:"scope,omitempty"`
	ShadowedBy string             `json:"shadowed_by,omitempty"`
}

// TrustPolicy decides whether a discovered skill is allowed to load.
type TrustPolicy func(scope SkillScope, skillPath string) (allowed bool, reason string)
