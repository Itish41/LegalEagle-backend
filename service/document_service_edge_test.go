package services

import (
	"errors"
	"testing"
	"time"

	"github.com/Itish41/LegalEagle/models"
	"github.com/agiledragon/gomonkey/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func TestDocumentServiceEdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*MockDB)
		action     func(*TestDocumentService) error
		wantErr    bool
		assertions func(*testing.T, *MockDB)
	}{
		{
			name: "CreateActionItems - Invalid JSON",
			setup: func(m *MockDB) {
				// No DB calls expected due to early JSON error
			},
			action: func(s *TestDocumentService) error {
				doc := models.Document{
					ID:         "doc1",
					ParsedData: datatypes.JSON([]byte(`invalid json`)),
				}
				return s.CreateActionItems(doc)
			},
			wantErr: true,
			assertions: func(t *testing.T, m *MockDB) {
				m.AssertNotCalled(t, "Where")
			},
		},
		{
			name: "GetPendingActionItemsWithTitles - DB Error",
			setup: func(m *MockDB) {
				m.On("Where", "status = ?", []interface{}{"pending"}).
					Return(m)
				m.On("Find", mock.Anything, mock.Anything).
					Return(m)
				m.On("Error").Return(errors.New("db error"))
			},
			action: func(s *TestDocumentService) error {
				_, err := s.GetPendingActionItemsWithTitles()
				return err
			},
			wantErr: true,
			assertions: func(t *testing.T, m *MockDB) {
				m.AssertExpectations(t)
			},
		},
		{
			name: "UpdateActionItem - Action Not Found",
			setup: func(m *MockDB) {
				m.On("First", mock.Anything, []interface{}{"id = ?", "999"}).
					Return(m)
				m.On("Error").Return(gorm.ErrRecordNotFound)
			},
			action: func(s *TestDocumentService) error {
				return s.UpdateActionItem("999")
			},
			wantErr: true,
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
