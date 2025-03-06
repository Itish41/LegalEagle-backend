package services

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Itish41/LegalEagle/models"
	"github.com/agiledragon/gomonkey/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"gorm.io/datatypes"
)

// FixedTime for consistent time patching
var FixedTime = time.Date(2025, time.March, 5, 0, 0, 0, 0, time.UTC)

// DBInterface defines GORM-like methods for mocking
type DBInterface interface {
	Where(query interface{}, args ...interface{}) DBInterface
	First(dest interface{}, conds ...interface{}) DBInterface
	Create(value interface{}) DBInterface
	Omit(fields ...string) DBInterface
	Updates(values interface{}) DBInterface
	Save(value interface{}) DBInterface
	Model(value interface{}) DBInterface
	Select(query interface{}, args ...interface{}) DBInterface
	Find(dest interface{}, conds ...interface{}) DBInterface
	Error() error
}

// MockDB implements DBInterface with testify/mock
type MockDB struct {
	mock.Mock
}

func (m *MockDB) Where(query interface{}, args ...interface{}) DBInterface {
	m.Called(query, args)
	return m
}

func (m *MockDB) First(dest interface{}, conds ...interface{}) DBInterface {
	m.Called(dest, conds)
	return m
}

func (m *MockDB) Create(value interface{}) DBInterface {
	m.Called(value)
	return m
}

func (m *MockDB) Omit(fields ...string) DBInterface {
	m.Called(fields)
	return m
}

func (m *MockDB) Updates(values interface{}) DBInterface {
	m.Called(values)
	return m
}

func (m *MockDB) Save(value interface{}) DBInterface {
	m.Called(value)
	return m
}

func (m *MockDB) Model(value interface{}) DBInterface {
	m.Called(value)
	return m
}

func (m *MockDB) Select(query interface{}, args ...interface{}) DBInterface {
	m.Called(query, args)
	return m
}

func (m *MockDB) Find(dest interface{}, conds ...interface{}) DBInterface {
	m.Called(dest, conds)
	return m
}

func (m *MockDB) Error() error {
	args := m.Called()
	return args.Error(0)
}

// TestDocumentService uses DBInterface instead of *gorm.DB
type TestDocumentService struct {
	db DBInterface
}

func (s *TestDocumentService) CreateActionItems(doc models.Document) error {
	var results []map[string]interface{}
	if err := json.Unmarshal([]byte(doc.ParsedData), &results); err != nil {
		return err
	}

	for _, result := range results {
		status, ok := result["status"].(string)
		if !ok || status != "fail" {
			continue
		}

		ruleName, ok := result["rule_name"].(string)
		if !ok {
			continue
		}

		var rule models.ComplianceRule
		if err := s.db.Where("name = ?", ruleName).First(&rule).Error(); err != nil {
			continue
		}

		explanation, _ := result["explanation"].(string)
		severity, _ := result["severity"].(string)
		action := models.ActionItem{
			DocumentID:  doc.ID,
			RuleID:      rule.ID,
			Description: "Address " + ruleName + " non-compliance: " + explanation,
			Priority:    strings.Title(strings.ToLower(severity)),
			Status:      "pending",
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
			DueDate:     time.Now().AddDate(0, 1, 0),
		}

		if err := s.db.Omit("AssignedTo").Create(&action).Error(); err != nil {
			return err
		}

		docResult := models.DocumentRuleResult{
			DocumentID: doc.ID,
			RuleID:     rule.ID,
			Status:     "fail",
			Details:    datatypes.JSON(marshallResult(result)),
			CreatedAt:  time.Now(),
		}
		if err := s.db.Create(&docResult).Error(); err != nil {
			return err
		}
	}
	return nil
}

func (s *TestDocumentService) GetPendingActionItemsWithTitles() ([]map[string]interface{}, error) {
	var items []models.ActionItem
	if err := s.db.Where("status = ?", "pending").Find(&items).Error(); err != nil {
		return nil, err
	}

	result := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		var doc models.Document
		if err := s.db.Select("title").Where("id = ?", item.DocumentID).First(&doc).Error(); err != nil {
			continue
		}
		result = append(result, map[string]interface{}{
			"id":          item.ID,
			"document_id": item.DocumentID,
			"title":       doc.Title,
			"rule_id":     item.RuleID,
			"description": item.Description,
			"priority":    item.Priority,
			"due_date":    item.DueDate,
			"status":      item.Status,
		})
	}
	return result, nil
}

func (s *TestDocumentService) UpdateActionItem(actionID string) error {
	var action models.ActionItem
	if err := s.db.First(&action, "id = ?", actionID).Error(); err != nil {
		return err
	}

	action.Status = "completed"
	action.UpdatedAt = time.Now()

	if err := s.db.Model(&action).Omit("AssignedTo").Updates(map[string]interface{}{
		"Status":    "completed",
		"UpdatedAt": time.Now(),
	}).Error(); err != nil {
		return err
	}

	var docResult models.DocumentRuleResult
	if err := s.db.Where("document_id = ? AND rule_id = ?", action.DocumentID, action.RuleID).First(&docResult).Error(); err != nil {
		return err
	}

	docResult.Status = "resolved"
	details := make(map[string]interface{})
	if len(docResult.Details) > 0 {
		if err := json.Unmarshal(docResult.Details, &details); err != nil {
			details = make(map[string]interface{})
		}
	}
	details["explanation"] = "No issues"
	updatedDetails, _ := json.Marshal(details)
	docResult.Details = datatypes.JSON(updatedDetails)
	docResult.CreatedAt = time.Now()

	if err := s.db.Save(&docResult).Error(); err != nil {
		return err
	}

	var doc models.Document
	if err := s.db.First(&doc, "id = ?", action.DocumentID).Error(); err != nil {
		return err
	}

	parsedData := make(map[string]interface{})
	if len(doc.ParsedData) > 0 {
		if err := json.Unmarshal(doc.ParsedData, &parsedData); err != nil {
			parsedData = make(map[string]interface{})
		}
	}
	parsedData["status"] = true
	updatedParsedData, _ := json.Marshal(parsedData)

	if err := s.db.Model(&doc).Updates(map[string]interface{}{
		"ParsedData": updatedParsedData,
		"UpdatedAt":  time.Now(),
	}).Error(); err != nil {
		return err
	}

	return nil
}

func marshallResult(result map[string]interface{}) []byte {
	bytes, _ := json.Marshal(result)
	return bytes
}

func TestDocumentServiceEval(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*MockDB)
		action     func(*TestDocumentService) error
		wantErr    bool
		assertions func(*testing.T, *MockDB)
	}{
		{
			name: "CreateActionItems - Success",
			setup: func(m *MockDB) {
				m.On("Where", "name = ?", []interface{}{"NDA Check"}).
					Return(m)
				m.On("First", mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						rule := args.Get(0).(*models.ComplianceRule)
						*rule = models.ComplianceRule{ID: "rule1", Name: "NDA Check"}
					}).
					Return(m)
				m.On("Omit", []string{"AssignedTo"}).
					Return(m)
				m.On("Create", mock.AnythingOfType("*models.ActionItem")).
					Return(m)
				m.On("Create", mock.AnythingOfType("*models.DocumentRuleResult")).
					Return(m)
				m.On("Error").Return(nil)
			},
			action: func(s *TestDocumentService) error {
				doc := models.Document{
					ID: "doc1",
					ParsedData: datatypes.JSON([]byte(`[
						{"rule_name": "NDA Check", "status": "fail", "explanation": "Missing NDA", "severity": "high"}
					]`)),
				}
				return s.CreateActionItems(doc)
			},
			wantErr: false,
			assertions: func(t *testing.T, m *MockDB) {
				m.AssertExpectations(t)
			},
		},
		{
			name: "GetPendingActionItemsWithTitles - Success",
			setup: func(m *MockDB) {
				m.On("Where", "status = ?", []interface{}{"pending"}).
					Return(m)
				m.On("Find", mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						items := args.Get(0).(*[]models.ActionItem)
						*items = []models.ActionItem{
							{
								ID:          "1",
								DocumentID:  "doc1",
								RuleID:      "rule1",
								Description: "Test action",
								Priority:    "High",
								Status:      "pending",
								DueDate:     FixedTime,
							},
						}
					}).
					Return(m)
				m.On("Select", "title", mock.Anything).
					Return(m)
				m.On("Where", "id = ?", []interface{}{"doc1"}).
					Return(m)
				m.On("First", mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						doc := args.Get(0).(*models.Document)
						*doc = models.Document{ID: "doc1", Title: "Test Doc"}
					}).
					Return(m)
				m.On("Error").Return(nil)
			},
			action: func(s *TestDocumentService) error {
				_, err := s.GetPendingActionItemsWithTitles()
				return err
			},
			wantErr: false,
			assertions: func(t *testing.T, m *MockDB) {
				m.AssertExpectations(t)
				s := &TestDocumentService{db: m}
				items, _ := s.GetPendingActionItemsWithTitles()
				assert.Len(t, items, 1)
				assert.Equal(t, "Test Doc", items[0]["title"])
			},
		},
		{
			name: "UpdateActionItem - Success",
			setup: func(m *MockDB) {
				// First call: Fetch ActionItem
				m.On("First", mock.Anything, []interface{}{"id = ?", "1"}).
					Run(func(args mock.Arguments) {
						action := args.Get(0).(*models.ActionItem)
						*action = models.ActionItem{ID: "1", DocumentID: "doc1", RuleID: "rule1", Status: "pending"}
					}).
					Return(m)
				m.On("Model", mock.Anything).
					Return(m)
				m.On("Omit", []string{"AssignedTo"}).
					Return(m)
				m.On("Updates", mock.Anything).
					Return(m)
				// Second call: Fetch DocumentRuleResult with Where().First()
				m.On("Where", "document_id = ? AND rule_id = ?", []interface{}{"doc1", "rule1"}).
					Return(m)
				m.On("First", mock.Anything, []interface{}(nil)). // Matches Where().First() with nil conditions
											Run(func(args mock.Arguments) {
						docResult := args.Get(0).(*models.DocumentRuleResult)
						*docResult = models.DocumentRuleResult{DocumentID: "doc1", RuleID: "rule1", Status: "fail"}
					}).
					Return(m)
				m.On("Save", mock.Anything).
					Return(m)
				// Third call: Fetch Document
				m.On("First", mock.Anything, []interface{}{"id = ?", "doc1"}).
					Run(func(args mock.Arguments) {
						doc := args.Get(0).(*models.Document)
						*doc = models.Document{ID: "doc1", ParsedData: datatypes.JSON([]byte(`{"status": false}`))}
					}).
					Return(m)
				m.On("Model", mock.Anything).
					Return(m)
				m.On("Updates", mock.Anything).
					Return(m)
				m.On("Error").Return(nil)
			},
			action: func(s *TestDocumentService) error {
				return s.UpdateActionItem("1")
			},
			wantErr: false,
			assertions: func(t *testing.T, m *MockDB) {
				m.AssertExpectations(t)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Patch time.Now()
			patches := gomonkey.ApplyFunc(time.Now, func() time.Time {
				return FixedTime
			})
			defer patches.Reset()

			mockDB := new(MockDB)
			tt.setup(mockDB)
			s := &TestDocumentService{db: mockDB}
			err := tt.action(s)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			tt.assertions(t, mockDB)
		})
	}
}
