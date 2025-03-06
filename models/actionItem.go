package models

import "time"

type ActionItem struct {
	ID          string `gorm:"type:uuid;primaryKey;default:gen_random_uuid()"`
	DocumentID  string `gorm:"type:uuid"`
	RuleID      string `gorm:"type:uuid"`
	Description string `gorm:"not null"`
	AssignedTo  string `gorm:"type:string"`
	Status      string
	Priority    string
	DueDate     time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
