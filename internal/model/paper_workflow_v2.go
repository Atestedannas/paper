package model

import (
	"time"

	"github.com/google/uuid"
)

type CompiledTemplate struct {
	ID                    uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	SchoolID              string    `gorm:"size:64;index;not null" json:"school_id"`
	TemplateName          string    `gorm:"size:255;not null" json:"template_name"`
	TemplateVersion       string    `gorm:"size:64;index;not null" json:"template_version"`
	SourceFilePath        string    `gorm:"size:255;not null" json:"source_file_path"`
	SkeletonPath          string    `gorm:"size:255;not null" json:"skeleton_path"`
	ManifestJSON          string    `gorm:"type:jsonb;not null" json:"manifest_json"`
	BlockCatalogJSON      string    `gorm:"type:jsonb;not null" json:"block_catalog_json"`
	StyleProfilesJSON     string    `gorm:"type:jsonb;not null" json:"style_profiles_json"`
	MappingContractJSON   string    `gorm:"type:jsonb;not null" json:"mapping_contract_json"`
	VerificationRulesJSON string    `gorm:"type:jsonb;not null" json:"verification_rules_json"`
	PatchTargetsJSON      string    `gorm:"type:jsonb;not null" json:"patch_targets_json"`
	Status                string    `gorm:"size:32;not null;default:'compiled'" json:"status"`
	CreatedAt             time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt             time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`

	Jobs []PaperWorkflowJob `gorm:"foreignKey:CompiledTemplateID" json:"jobs,omitempty"`
}

type PaperWorkflowJob struct {
	ID                 uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	PaperID            uuid.UUID `gorm:"type:uuid;index;not null" json:"paper_id"`
	UserID             uuid.UUID `gorm:"type:uuid;index;not null" json:"user_id"`
	CompiledTemplateID uuid.UUID `gorm:"type:uuid;index;not null" json:"compiled_template_id"`
	Status             string    `gorm:"size:32;not null;default:'uploaded'" json:"status"`
	Stage              string    `gorm:"size:32;not null;default:'queued'" json:"stage"`
	DownloadPath       string    `gorm:"size:255" json:"download_path"`
	VerifyResultJSON   string    `gorm:"type:jsonb;not null;default:'{}'" json:"verify_result_json"`
	CreatedAt          time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`
	UpdatedAt          time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"updated_at"`

	Paper            Paper                `gorm:"foreignKey:PaperID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT;" json:"paper,omitempty"`
	User             User                 `gorm:"foreignKey:UserID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT;" json:"user,omitempty"`
	CompiledTemplate CompiledTemplate     `gorm:"foreignKey:CompiledTemplateID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT;" json:"compiled_template,omitempty"`
	Issues           []PaperWorkflowIssue `gorm:"foreignKey:JobID" json:"issues,omitempty"`
}

type PaperWorkflowIssue struct {
	ID         uuid.UUID `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	JobID      uuid.UUID `gorm:"type:uuid;index;not null" json:"job_id"`
	Kind       string    `gorm:"size:32;index;not null" json:"kind"`
	Severity   string    `gorm:"size:16;index;not null" json:"severity"`
	BlockID    string    `gorm:"size:128" json:"block_id"`
	Message    string    `gorm:"type:text;not null" json:"message"`
	DetailJSON string    `gorm:"type:jsonb;not null;default:'{}'" json:"detail_json"`
	CreatedAt  time.Time `gorm:"default:CURRENT_TIMESTAMP" json:"created_at"`

	Job PaperWorkflowJob `gorm:"foreignKey:JobID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"job,omitempty"`
}
