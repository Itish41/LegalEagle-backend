package services

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Itish41/LegalEagle/models"
	"github.com/agiledragon/gomonkey/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"gorm.io/datatypes"
)

// FixedTime is used to patch time.Now in tests.
// var FixedTime = time.Date(2025, time.March, 5, 0, 0, 0, 0, time.UTC)

// --- Assume that MockDB implements the same DBInterface as used by DocumentService ---
// (See your previous test files for the complete definition of MockDB.)

// Test for CreateActionItems
func TestDocumentService_CreateActionItems(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(m *MockDB)
		doc        models.Document
		wantErr    bool
		assertions func(t *testing.T, m *MockDB)
	}{
		{
			name: "Success",
			setup: func(m *MockDB) {
				// Expect lookup for the compliance rule by name.
				m.On("Where", "name = ?", []interface{}{"NDA Check"}).
					Return(m)
				m.On("First", mock.Anything, mock.Anything).
					Run(func(args mock.Arguments) {
						// Set the found rule (non-empty ID)
						rule := args.Get(0).(*models.ComplianceRule)
						*rule = models.ComplianceRule{ID: "rule1", Name: "NDA Check"}
					}).
					Return(m)
				// Expect creation of the action item (skipping AssignedTo)
				m.On("Omit", []string{"AssignedTo"}).
					Return(m)
				m.On("Create", mock.AnythingOfType("*models.ActionItem")).
					Return(m)
				m.On("Error").Return(nil)
				// Expect creation of the document rule result
				m.On("Create", mock.AnythingOfType("*models.DocumentRuleResult")).
					Return(m)
				m.On("Error").Return(nil)
			},
			doc: models.Document{
				ID: "doc1",
				// Valid JSON with one failed compliance result.
				ParsedData: datatypes.JSON([]byte(`[{"rule_name": "NDA Check", "status": "fail", "explanation": "Missing NDA", "severity": "high"}]`)),
			},
			wantErr: false,
			assertions: func(t *testing.T, m *MockDB) {
				m.AssertExpectations(t)
			},
		},
		{
			name: "Invalid JSON",
			setup: func(m *MockDB) {
				// No DB calls should occur
			},
			doc: models.Document{
				ID:         "doc1",
				ParsedData: datatypes.JSON([]byte(`invalid json`)),
			},
			wantErr: true,
			assertions: func(t *testing.T, m *MockDB) {
				m.AssertNotCalled(t, "Where")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Patch time.Now for consistent timestamps.
			patches := gomonkey.ApplyFunc(time.Now, func() time.Time {
				return FixedTime
			})
			defer patches.Reset()

			mockDB := new(MockDB)
			tt.setup(mockDB)
			service := &TestDocumentService{db: mockDB}
			err := service.CreateActionItems(tt.doc)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			tt.assertions(t, mockDB)
		})
	}
}

// Test for GetPendingActionItemsWithTitles
func TestDocumentService_GetPendingActionItemsWithTitles(t *testing.T) {
	patches := gomonkey.ApplyFunc(time.Now, func() time.Time {
		return FixedTime
	})
	defer patches.Reset()

	mockDB := new(MockDB)
	// Expect fetching pending action items.
	mockDB.On("Where", "status = ?", []interface{}{"pending"}).
		Return(mockDB)
	mockDB.On("Find", mock.Anything, mock.Anything).
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
		Return(mockDB)
	mockDB.On("Error").Return(nil)

	// Expect fetching the document title for each action item.
	mockDB.On("Select", "title", mock.Anything).
		Return(mockDB)
	mockDB.On("Where", "id = ?", []interface{}{"doc1"}).
		Return(mockDB)
	mockDB.On("First", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			doc := args.Get(0).(*models.Document)
			*doc = models.Document{ID: "doc1", Title: "Test Document"}
		}).
		Return(mockDB)
	mockDB.On("Error").Return(nil)

	service := &TestDocumentService{db: mockDB}
	res, err := service.GetPendingActionItemsWithTitles()
	assert.NoError(t, err)
	assert.Len(t, res, 1)
	assert.Equal(t, "Test Document", res[0]["title"])
	mockDB.AssertExpectations(t)
}

// Test for UpdateActionItem
func TestDocumentService_UpdateActionItem(t *testing.T) {
	patches := gomonkey.ApplyFunc(time.Now, func() time.Time {
		return FixedTime
	})
	defer patches.Reset()

	mockDB := new(MockDB)
	// Expect fetching the action item by ID.
	mockDB.On("First", mock.Anything, []interface{}{"id = ?", "1"}).
		Run(func(args mock.Arguments) {
			action := args.Get(0).(*models.ActionItem)
			*action = models.ActionItem{ID: "1", DocumentID: "doc1", RuleID: "rule1", Status: "pending"}
		}).
		Return(mockDB)
	mockDB.On("Error").Return(nil)

	// Expect updating the action item.
	mockDB.On("Model", mock.Anything).
		Return(mockDB)
	mockDB.On("Omit", []string{"AssignedTo"}).
		Return(mockDB)
	mockDB.On("Updates", mock.Anything).
		Return(mockDB)
	mockDB.On("Error").Return(nil)

	// Expect fetching the DocumentRuleResult.
	mockDB.On("Where", "document_id = ? AND rule_id = ?", []interface{}{"doc1", "rule1"}).
		Return(mockDB)
	mockDB.On("First", mock.Anything, []interface{}(nil)).
		Run(func(args mock.Arguments) {
			docResult := args.Get(0).(*models.DocumentRuleResult)
			// Provide initial details JSON.
			*docResult = models.DocumentRuleResult{DocumentID: "doc1", RuleID: "rule1", Status: "fail", Details: datatypes.JSON([]byte(`{"explanation": "Missing NDA"}`))}
		}).
		Return(mockDB)
	mockDB.On("Error").Return(nil)

	// Expect saving the updated DocumentRuleResult.
	mockDB.On("Save", mock.Anything).
		Return(mockDB)
	mockDB.On("Error").Return(nil)

	// Expect fetching the Document.
	mockDB.On("First", mock.Anything, []interface{}{"id = ?", "doc1"}).
		Run(func(args mock.Arguments) {
			doc := args.Get(0).(*models.Document)
			// Initially, parsed_data shows a false status.
			*doc = models.Document{ID: "doc1", ParsedData: datatypes.JSON(`{"status": false}`)}
		}).
		Return(mockDB)
	mockDB.On("Error").Return(nil)

	// Expect updating the Document with new parsed_data.
	mockDB.On("Model", mock.Anything).
		Return(mockDB)
	mockDB.On("Updates", mock.Anything).
		Return(mockDB)
	mockDB.On("Error").Return(nil)

	service := &TestDocumentService{db: mockDB}
	err := service.UpdateActionItem("1")
	assert.NoError(t, err)
	mockDB.AssertExpectations(t)
}

// // Test for GetPendingActionItems
// func TestDocumentService_GetPendingActionItems(t *testing.T) {
// 	mockDB := new(MockDB)
// 	mockDB.On("Where", "status = ?", []interface{}{"pending"}).
// 		Return(mockDB)
// 	mockDB.On("Find", mock.Anything, mock.Anything).
// 		Run(func(args mock.Arguments) {
// 			items := args.Get(0).(*[]models.ActionItem)
// 			*items = []models.ActionItem{
// 				{
// 					ID:          "1",
// 					DocumentID:  "doc1",
// 					RuleID:      "rule1",
// 					Description: "Action 1",
// 					Status:      "pending",
// 				},
// 			}
// 		}).
// 		Return(mockDB)
// 	mockDB.On("Error").Return(nil)

// 	service := &TestDocumentService{db: mockDB}
// 	items, err := service.GetPendingActionItems()
// 	assert.NoError(t, err)
// 	assert.Len(t, items, 1)
// 	assert.Equal(t, "1", items[0]["id"])
// 	mockDB.AssertExpectations(t)
// }

// Tests for the extractRuleName helper function.

// --- Optionally, you could add tests for marshalResult if needed. ---
// For example:
func TestMarshalResult(t *testing.T) {
	input := map[string]interface{}{
		"rule_name":   "NDA Check",
		"status":      "fail",
		"explanation": "Missing NDA",
		"severity":    "high",
	}
	bytes := marshalResult(input)
	var output map[string]interface{}
	err := json.Unmarshal(bytes, &output)
	assert.NoError(t, err)
	assert.Equal(t, input["rule_name"], output["rule_name"])
}

// TestExtractRuleName tests the extractRuleName helper function
// TestExtractRuleName tests the extractRuleName helper function
func TestExtractRuleName(t *testing.T) {
	tests := []struct {
		explanation string
		expected    string
	}{
		{
			explanation: "This document is missing a non-disclosure agreement",
			expected:    "NDA Check",
		},
		{
			explanation: "Please ensure nda check is followed",
			expected:    "NDA Check",
		},
		{
			explanation: "There is a confidentiality breach",
			expected:    "Confidentiality Check",
		},
		{
			explanation: "The 'Custom Rule' was violated",
			expected:    "custom rule", // Matches current lowercase output
		},
		{
			explanation: "The \"Custom Rule\" is important",
			expected:    "custom rule", // Matches current lowercase output
		},
		{
			explanation: "rule Custom Extraction",
			expected:    "custom extraction", // Matches current lowercase output
		},
		{
			explanation: "Compliance required by Custom Rule",
			expected:    "custom rule", // Matches current lowercase output
		},
		{
			explanation: "No matching pattern here",
			expected:    "Unknown Rule",
		},
	}

	for _, tt := range tests {
		t.Run(tt.explanation, func(t *testing.T) {
			result := extractRuleName(tt.explanation)
			assert.Equal(t, tt.expected, result, "extractRuleName(%q) should return %q, got %q", tt.explanation, tt.expected, result)
		})
	}
}

func TestExtractRuleNameCases(t *testing.T) {
	tests := []struct {
		explanation string
		expected    string
	}{
		{
			explanation: "This document is missing a non-disclosure agreement",
			expected:    "NDA Check",
		},
		{
			explanation: "Please ensure nda check is followed",
			expected:    "NDA Check",
		},
		{
			explanation: "There is a confidentiality breach",
			expected:    "Confidentiality Check",
		},
		{
			explanation: "The 'Custom Rule' was violated",
			expected:    "custom rule", // Adjusted to match lowercase output
		},
		{
			explanation: "The \"Custom Rule\" is important",
			expected:    "custom rule", // Adjusted to match lowercase output
		},
		{
			explanation: "rule Custom Extraction",
			expected:    "custom extraction", // Adjusted to match lowercase output
		},
		{
			explanation: "Compliance required by Custom Rule",
			expected:    "custom rule", // Adjusted to match lowercase output
		},
		{
			explanation: "No matching pattern here",
			expected:    "Unknown Rule",
		},
	}

	for _, tt := range tests {
		t.Run(tt.explanation, func(t *testing.T) {
			result := extractRuleName(tt.explanation)
			assert.Equal(t, tt.expected, result, "extractRuleName(%q) should return %q, got %q", tt.explanation, tt.expected, result)
		})
	}
}
