package models

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Document represents a legal document with fields for database and search indexing.
type Document struct {
	// ID is a unique identifier for the document, stored as a UUID in the database.
	// In Elasticsearch, it's indexed as a keyword for exact matching.
	ID string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" elastic:"type:keyword"`

	// Title is the document's title, indexed as text for full-text search.
	Title string `elastic:"type:text,analyzer:standard"`

	// FileType indicates the type of the file (e.g., "pdf", "docx"), indexed as a keyword.
	FileType string `elastic:"type:keyword"`

	// OriginalURL is the S3 URL where the original file is stored, indexed as a keyword.
	OriginalURL string `elastic:"type:keyword"`

	// OcrText contains the text extracted via OCR, indexed as text for full-text search.
	OcrText string `elastic:"type:text,analyzer:standard"`

	// ParsedData is a JSONB field for structured data (e.g., clauses), indexed as an object.
	ParsedData datatypes.JSON `elastic:"type:object"`

	// RiskScore is a calculated score for compliance risk, indexed as a float.
	RiskScore float64 `elastic:"type:float"`

	// CreatedAt and UpdatedAt track when the document was created and last updated, indexed as dates.
	CreatedAt time.Time `elastic:"type:date"`
	UpdatedAt time.Time `elastic:"type:date"`

	// SearchContent is a computed field for full-text search, combining Title and OcrText.
	// It's not stored in the database (gorm:"-") but is indexed in Elasticsearch.
	SearchContent string `gorm:"-" elastic:"type:text,analyzer:standard"`
}

// BeforeSave is a GORM hook to populate SearchContent before saving to Elasticsearch.
func (d *Document) BeforeSave(tx *gorm.DB) error {
	// Combine Title and OcrText for full-text search.
	d.SearchContent = d.Title + " " + d.OcrText
	return nil
}
