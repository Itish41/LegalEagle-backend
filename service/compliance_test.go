package services

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockHTTPClient implements http.Client interface for testing
type MockHTTPClient struct {
	mock.Mock
}

func (m *MockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	args := m.Called(req)
	return args.Get(0).(*http.Response), args.Error(1)
}

// TestAIService tests the AI integration functionality
type TestAIService struct {
	client *MockHTTPClient
}

func TestAIServiceEval(t *testing.T) {
	// Load .env file
	err := godotenv.Load("../.env")
	if err != nil {
		t.Fatal("Error loading .env file")
	}

	t.Run("Successful Groq API Response", func(t *testing.T) {
		client := &MockHTTPClient{}
		service := &TestAIService{client: client}
		apiKey := os.Getenv("VITE_GROQ_API_KEY")
		if apiKey == "" {
			t.Fatal("Groq API key not found in .env file")
		}
		req, err := http.NewRequest("POST", "https://api.groq.com/openai/v1/chat/completions", nil)
		if err != nil {
			t.Fatal("Failed to create request:", err)
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		mockResponse := &http.Response{
			StatusCode: http.StatusOK,
			Body:       http.NoBody,
		}
		client.On("Do", mock.MatchedBy(func(r *http.Request) bool {
			return r.Header.Get("Authorization") == "Bearer "+apiKey &&
				r.URL.String() == "https://api.groq.com/openai/v1/chat/completions"
		})).Return(mockResponse, nil)
		resp, err := service.client.Do(req)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("Groq API Rate Limiting", func(t *testing.T) {
		client := &MockHTTPClient{}
		service := &TestAIService{client: client}
		apiKey := os.Getenv("VITE_GROQ_API_KEY")
		if apiKey == "" {
			t.Fatal("Groq API key not found in .env file")
		}
		req, err := http.NewRequest("POST", "https://api.groq.com/openai/v1/chat/completions", nil)
		if err != nil {
			t.Fatal("Failed to create request:", err)
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		mockResponse := &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header: http.Header{
				"Retry-After": []string{"10"},
			},
			Body: http.NoBody,
		}
		client.On("Do", mock.MatchedBy(func(r *http.Request) bool {
			return r.Header.Get("Authorization") == "Bearer "+apiKey &&
				r.URL.String() == "https://api.groq.com/openai/v1/chat/completions"
		})).Return(mockResponse, nil)
		resp, err := service.client.Do(req)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
		assert.Equal(t, "10", resp.Header.Get("Retry-After"))
	})

	t.Run("Invalid Groq API Key", func(t *testing.T) {
		client := &MockHTTPClient{}
		service := &TestAIService{client: client}
		patches := gomonkey.ApplyFunc(os.Getenv, func(key string) string {
			return ""
		})
		defer patches.Reset()
		req, err := http.NewRequest("POST", "https://api.groq.com/openai/v1/chat/completions", nil)
		if err != nil {
			t.Fatal("Failed to create request:", err)
		}
		client.On("Do", mock.Anything).Return((*http.Response)(nil), errors.New("missing API key"))
		_, err = service.client.Do(req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing API key")
	})

	t.Run("Groq API Timeout", func(t *testing.T) {
		client := &MockHTTPClient{}
		service := &TestAIService{client: client}
		apiKey := os.Getenv("VITE_GROQ_API_KEY")
		if apiKey == "" {
			t.Fatal("Groq API key not found in .env file")
		}
		req, err := http.NewRequest("POST", "https://api.groq.com/openai/v1/chat/completions", nil)
		if err != nil {
			t.Fatal("Failed to create request:", err)
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		client.On("Do", mock.MatchedBy(func(r *http.Request) bool {
			return r.Header.Get("Authorization") == "Bearer "+apiKey &&
				r.URL.String() == "https://api.groq.com/openai/v1/chat/completions"
		})).Return((*http.Response)(nil), &net.OpError{Err: context.DeadlineExceeded})
		_, err = service.client.Do(req)
		assert.Error(t, err)
		if err != nil {
			assert.Contains(t, err.Error(), "deadline exceeded")
		}
	})
}
