package models

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// DocumentRuleResult stores the outcome of applying a compliance rule to a document.
type DocumentRuleResult struct {
	// ID is a unique identifier for the result, stored as a UUID in the database.
	// In Elasticsearch, it's indexed as a keyword for exact matching.
	ID string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" elastic:"type:keyword"`

	// DocumentID references the document being checked, indexed as a keyword.
	DocumentID string `gorm:"type:uuid" elastic:"type:keyword"`

	// RuleID references the compliance rule applied, indexed as a keyword.
	RuleID string `gorm:"type:uuid" elastic:"type:keyword"`

	// Status indicates whether the document passed or failed the rule (e.g., 'pass', 'fail'), indexed as a keyword.
	Status string `elastic:"type:keyword"`

	// Details is a JSONB field for additional information (e.g., matched text), indexed as an object.
	Details datatypes.JSON `elastic:"type:object"`

	// CreatedAt tracks when the result was recorded, indexed as a date.
	CreatedAt time.Time `elastic:"type:date"`

	// SearchSummary is a computed field for full-text search, summarizing the result.
	// It's not stored in the database but is indexed in Elasticsearch.
	SearchSummary string `gorm:"-" elastic:"type:text,analyzer:standard"`
}

// BeforeSave is a GORM hook to populate SearchSummary before saving to Elasticsearch.
func (dr *DocumentRuleResult) BeforeSave(tx *gorm.DB) error {
	// Combine Status and a summary from Details for full-text search.
	// Adjust this based on your Details structure (e.g., extract specific fields from JSON).
	dr.SearchSummary = dr.Status + " " + string(dr.Details)
	return nil
}
