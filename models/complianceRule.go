package models

import (
	"time"

	"gorm.io/gorm"
)

// ComplianceRule defines a rule for checking document compliance.
type ComplianceRule struct {
	// ID is a unique identifier for the rule, stored as a UUID in the database.
	// In Elasticsearch, it's indexed as a keyword for exact matching.
	ID string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" elastic:"type:keyword"`

	// Name is the rule's name, required and indexed as text for search.
	Name string `gorm:"not null" elastic:"type:text,analyzer:standard"`

	// Description provides details about the rule, indexed as text.
	Description string `elastic:"type:text,analyzer:standard"`

	// Pattern is the regex or keyword pattern for the rule, indexed as a keyword.
	Pattern string `elastic:"type:keyword"`

	// Severity indicates the rule's importance (e.g., 'low', 'medium', 'high'), indexed as a keyword.
	Severity string `elastic:"type:keyword"`

	// CreatedAt tracks when the rule was created, indexed as a date.
	CreatedAt time.Time `elastic:"type:date"`

	// SearchContent is a computed field for full-text search, combining Name and Description.
	// It's not stored in the database but is indexed in Elasticsearch.
	SearchContent string `gorm:"-" elastic:"type:text,analyzer:standard"`
}

// BeforeSave is a GORM hook to populate SearchContent before saving to Elasticsearch.
func (r *ComplianceRule) BeforeSave(tx *gorm.DB) error {
	// Combine Name and Description for full-text search.
	r.SearchContent = r.Name + " " + r.Description
	return nil
}
