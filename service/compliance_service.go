package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	// "github.com/Itish41/LegalEagle/models
	model "github.com/Itish41/LegalEagle/models"
)

// RateLimiter struct to manage API call rate limiting
type RateLimiter struct {
	mu           sync.Mutex
	requestCount map[string]int
	limit        int
	window       time.Duration
	lastReset    time.Time
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		requestCount: make(map[string]int),
		limit:        limit,
		window:       window,
		lastReset:    time.Now(),
	}
}

// Allow checks if a request is allowed based on rate limit
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Reset counter if window has passed
	if time.Since(rl.lastReset) > rl.window {
		rl.requestCount = make(map[string]int)
		rl.lastReset = time.Now()
	}

	// Increment and check count
	rl.requestCount[key]++
	return rl.requestCount[key] <= rl.limit
}

// Global rate limiters for different operations
var (
	groqRateLimiter = NewRateLimiter(50, 1*time.Minute)  // 50 Groq API calls per minute
	ruleRateLimiter = NewRateLimiter(100, 1*time.Minute) // 100 rule-related operations per minute
)

func (s *DocumentService) AddComplianceRule(rule *model.ComplianceRule) error {
	// Rate limit rule additions
	if !ruleRateLimiter.Allow("rule_addition") {
		return fmt.Errorf("rate limit exceeded for rule additions")
	}

	if err := s.db.Create(rule).Error; err != nil {
		log.Printf("Error saving compliance rule: %v", err)
		return err
	}
	log.Printf("Compliance rule %s added successfully", rule.Name)
	return nil
}

// DetermineApplicableRules uses Groq to suggest relevant rules
func (s *DocumentService) DetermineApplicableRules(ocrText string) ([]string, error) {
	// Rate limit Groq API calls
	if !groqRateLimiter.Allow("groq_api_call") {
		log.Println("Rate limit exceeded for Groq API calls locally")
		return s.fallbackRuleExtraction(ocrText, nil), nil
	}

	// Fetch all rules from the database
	allRules, err := s.GetAllComplianceRules()
	if err != nil {
		log.Printf("ERROR retrieving compliance rules: %v", err)
		return nil, err
	}
	log.Printf("Retrieved %d compliance rules from database", len(allRules))

	// Build rule details and names
	var ruleDetails []string
	ruleNames := make([]string, len(allRules))
	for i, rule := range allRules {
		ruleDetails = append(ruleDetails, fmt.Sprintf("%s: %s (Pattern: %s)", rule.Name, rule.Description, rule.Pattern))
		ruleNames[i] = rule.Name
	}
	log.Println("Rule details for Groq: ", ruleDetails)

	// Validate Groq API Key
	groqAPIKey := os.Getenv("VITE_GROQ_API_KEY")
	if groqAPIKey == "" {
		log.Println("ERROR: VITE_GROQ_API_KEY environment variable is not set")
		return nil, fmt.Errorf("VITE_GROQ_API_KEY environment variable is not set")
	}

	// Construct prompt
	prompt := fmt.Sprintf(`
    Analyze the following document text and determine which legal compliance rules from this list are violated:
    %s

    Document Text:
    %s

    Instructions:
    1. Carefully review the document text against each rule's description and pattern.
    2. Identify rules where the document fails to meet the requirements.
    3. Return a JSON object with a "violated_rules" array containing only the names of violated rules.
    4. If no rules are violated, return an empty array.
    5. Ensure rule names match exactly as provided.

    Response Format:
    {
        "violated_rules": ["Rule1", "Rule2", ...]
    }
    `, strings.Join(ruleDetails, "\n"), ocrText)
	log.Printf("Groq API Prompt: %s", prompt)

	// Prepare request body
	reqBody, err := json.Marshal(map[string]interface{}{
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"model":       "llama-3.3-70b-versatile",
		"temperature": 0.7,
		"max_tokens":  250,
		"response_format": map[string]string{
			"type": "json_object",
		},
	})
	if err != nil {
		log.Printf("ERROR creating request body: %v", err)
		return s.fallbackRuleExtraction(ocrText, ruleNames), nil
	}

	// Retry logic for rate limiting
	const maxRetries = 3
	var resp *http.Response
	for attempt := 0; attempt < maxRetries; attempt++ {
		req, err := http.NewRequest("POST", "https://api.groq.com/openai/v1/chat/completions", bytes.NewBuffer(reqBody))
		if err != nil {
			log.Printf("ERROR creating Groq request: %v", err)
			return nil, fmt.Errorf("failed to create Groq request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+groqAPIKey)
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err = client.Do(req)
		if err == nil && resp.StatusCode != 429 { // 429 is Too Many Requests
			break
		}
		if err != nil {
			log.Printf("ERROR sending Groq request (attempt %d): %v", attempt+1, err)
		} else if resp.StatusCode == 429 {
			log.Printf("Rate limit hit (attempt %d), status: %s", attempt+1, resp.Status)
			resp.Body.Close()
		}
		if attempt < maxRetries-1 {
			waitTime := time.Duration(10*(attempt+1)) * time.Second // Exponential backoff: 10s, 20s, 30s
			log.Printf("Retrying in %v...", waitTime)
			time.Sleep(waitTime)
		}
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		log.Printf("Non-200 status code: %d, status: %s", resp.StatusCode, resp.Status)
		return s.fallbackRuleExtraction(ocrText, ruleNames), nil
	}

	defer resp.Body.Close()

	// Read and parse response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("ERROR reading Groq response: %v", err)
		return s.fallbackRuleExtraction(ocrText, ruleNames), nil
	}
	log.Printf("Groq API Raw Response: %s", string(body))

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("ERROR parsing Groq response structure: %v", err)
		return s.fallbackRuleExtraction(ocrText, ruleNames), nil
	}

	var ruleResponse struct {
		ViolatedRules []string `json:"violated_rules"`
	}
	if len(result.Choices) > 0 {
		if err := json.Unmarshal([]byte(result.Choices[0].Message.Content), &ruleResponse); err != nil {
			log.Printf("ERROR parsing violated rules from content: %v", err)
			return s.fallbackRuleExtraction(ocrText, ruleNames), nil
		}
	}

	violatedRules := ruleResponse.ViolatedRules
	if len(violatedRules) == 0 {
		log.Println("No rules violated according to Groq")
	} else {
		// Validate rules exist in database
		validRules := make([]string, 0, len(violatedRules))
		for _, rule := range violatedRules {
			if contains(ruleNames, rule) {
				validRules = append(validRules, rule)
			} else {
				log.Printf("WARNING: Suggested violated rule '%s' not found in database rules", rule)
			}
		}
		violatedRules = validRules
		if len(violatedRules) == 0 {
			log.Println("No valid violated rules found after validation")
		}
	}

	log.Printf("Determined Violated Rules: %v", violatedRules)
	return violatedRules, nil
}

// Helper function for fallback rule extraction
func (s *DocumentService) fallbackRuleExtraction(ocrText string, ruleNames []string) []string {
	if ocrText == "" {
		return []string{}
	}

	// Normalize text for extremely flexible matching
	ocrLower := strings.ToLower(ocrText)
	ocrLower = strings.ReplaceAll(ocrLower, "-", " ")
	ocrLower = strings.ReplaceAll(ocrLower, "_", " ")
	ocrLower = regexp.MustCompile(`[^a-z0-9\s]`).ReplaceAllString(ocrLower, "")

	// Extremely broad rule matching criteria
	ruleMatchers := map[string][]string{
		"Confidentiality Marking": {
			"confidential", "private", "restricted", "secret", "sensitive",
		},
		"NDA Check": {
			"nda", "non-disclosure", "confidential", "agreement", "secret",
		},
		"Signature Requirement": {
			"sign", "signature", "date", "signed", "execute", "approval",
		},
		"Data Protection Clause": {
			"data", "privacy", "protection", "personal", "information", "secure",
		},
		"Liability Clause Requirement": {
			"liability", "responsibility", "limit", "clause", "legal", "risk",
		},
		"Payment Terms Specification": {
			"payment", "due", "terms", "money", "cost", "invoice", "charge",
		},
	}

	violated := []string{}

	for ruleName, matchTerms := range ruleMatchers {
		matchCount := 0
		for _, term := range matchTerms {
			if strings.Contains(ocrLower, term) {
				matchCount++
			}
		}

		// Extremely low threshold for matching
		if matchCount > 0 {
			log.Printf("Rule %s matched with %d terms", ruleName, matchCount)
			violated = append(violated, ruleName)
		}
	}

	if len(violated) == 0 {
		log.Println("No rules matched in extremely lax check")
	} else {
		log.Printf("Lax rule matching found violations: %v", violated)
	}

	return violated
}

// Helper function to remove duplicate strings
func removeDuplicates(slice []string) []string {
	seen := make(map[string]bool)
	result := []string{}
	for _, item := range slice {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}
	return result
}

// fuzzyContains checks if the text contains a minimum number of keywords
func fuzzyContains(text string, keywords ...string) bool {
	matchCount := 0
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			matchCount++
		}
	}
	// Require at least half of the keywords to match
	return matchCount >= len(keywords)/2+1
}

// fuzzyContainsAny checks if the text contains ANY of the given keywords
func fuzzyContainsAny(text string, keywords ...string) bool {
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

// DetermineApplicableRulesBatch processes multiple documents in batches
func (s *DocumentService) DetermineApplicableRulesBatch(documents []string, batchSize int) (map[string][]string, error) {
	// Validate input
	if len(documents) == 0 {
		return nil, fmt.Errorf("no documents provided for batch processing")
	}

	// Rate limit batch processing
	if !groqRateLimiter.Allow("batch_rule_determination") {
		return nil, fmt.Errorf("rate limit exceeded for batch rule determination")
	}

	// Fetch all rules from the database
	allRules, err := s.GetAllComplianceRules()
	if err != nil {
		log.Printf("ERROR retrieving compliance rules: %v", err)
		return nil, err
	}
	log.Printf("Retrieved %d compliance rules from database", len(allRules))

	// Prepare rule names for Groq
	ruleNames := make([]string, len(allRules))
	for _, rule := range allRules {
		ruleNames = append(ruleNames, rule.Name)
	}

	// Validate Groq API Key
	groqAPIKey := os.Getenv("VITE_GROQ_API_KEY")
	if groqAPIKey == "" {
		return nil, fmt.Errorf("VITE_GROQ_API_KEY environment variable is not set")
	}

	// Process documents in batches
	results := make(map[string][]string)
	var mu sync.Mutex

	// Process documents in batches
	for i := 0; i < len(documents); i += batchSize {
		end := i + batchSize
		if end > len(documents) {
			end = len(documents)
		}
		batchDocuments := documents[i:end]

		// Prepare batch request
		batchRequest := prepareBatchComplianceRequest(batchDocuments, ruleNames)

		// Send batch request to Groq
		batchResponse, err := sendBatchComplianceRequest(batchRequest, groqAPIKey)
		if err != nil {
			log.Printf("Error in batch compliance request: %v", err)
			continue
		}

		// Process batch results
		for docID, applicableRules := range batchResponse.Results {
			mu.Lock()
			// Validate suggested rules exist in database
			validRules := validateRules(applicableRules, ruleNames)

			// Ensure at least one rule is returned
			if len(validRules) == 0 {
				validRules = []string{"General Compliance"}
			}

			results[docID] = validRules
			mu.Unlock()
		}
	}

	return results, nil
}

// prepareBatchComplianceRequest creates a batch request for Groq
func prepareBatchComplianceRequest(documents []string, ruleNames []string) BatchComplianceRequest {
	batchDocuments := make([]DocumentComplianceCheck, len(documents))
	for i, doc := range documents {
		batchDocuments[i] = DocumentComplianceCheck{
			ID:      fmt.Sprintf("doc_%d", i),
			OCRText: doc,
		}
	}

	return BatchComplianceRequest{
		Documents: batchDocuments,
		RuleNames: ruleNames, // Keep ruleNames in scope
	}
}

// sendBatchComplianceRequest sends a batch request to Groq and processes the response
func sendBatchComplianceRequest(batchRequest BatchComplianceRequest, apiKey string) (*BatchComplianceResponse, error) {
	// Construct the detailed, structured prompt
	promptTemplate := `
	For each document, analyze the text and suggest the most relevant legal compliance rules from this list:
	%s

	Instructions:
	1. Carefully review each document text.
	2. Match the content to rules based on their names.
	3. Return a JSON object with an "results" map where keys are document IDs and values are arrays of applicable rule names.
	4. If no rules are clearly applicable for a document, return a minimal set of generic rules.
	5. Ensure rule names match exactly as provided.

	Response Format:
	{
		"results": {
			"doc_0": ["Rule1", "Rule2", ...],
			"doc_1": ["GeneralCompliance"],
			...
		}
	}
	`

	// Prepare request body
	reqBody, err := json.Marshal(map[string]interface{}{
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": fmt.Sprintf(promptTemplate, strings.Join(batchRequest.RuleNames, "\n")), // Use batchRequest.RuleNames
			},
		},
		"model":       "llama-3.3-70b-versatile",
		"temperature": 0.7,
		"max_tokens":  500,
		"response_format": map[string]string{
			"type": "json_object",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create request body: %w", err)
	}

	// Send request to Groq
	req, err := http.NewRequest("POST", "https://api.groq.com/openai/v1/chat/completions", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create Groq request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 60 * time.Second, // Increased timeout for batch processing
		Transport: &http.Transport{
			MaxIdleConns:        10,
			IdleConnTimeout:     60 * time.Second,
			DisableCompression:  true,
			TLSHandshakeTimeout: 15 * time.Second,
		},
	}

	// Execute request with retries
	var resp *http.Response
	for attempt := 0; attempt < 3; attempt++ {
		resp, err = client.Do(req)
		if err == nil {
			break
		}
		log.Printf("Groq API request attempt %d failed: %v", attempt+1, err)
		time.Sleep(time.Duration(attempt+1) * time.Second) // Exponential backoff
	}

	if err != nil {
		return nil, fmt.Errorf("failed to send Groq API request after 3 attempts: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("non-200 status code: %d, response: %s", resp.StatusCode, string(body))
	}

	// Read and parse response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read Groq response: %w", err)
	}
	log.Printf("Groq API Batch Response: %s", string(body))

	// Parse the response
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse Groq response structure: %w", err)
	}

	// Parse batch results
	var batchResponse BatchComplianceResponse
	if len(result.Choices) > 0 {
		if err := json.Unmarshal([]byte(result.Choices[0].Message.Content), &batchResponse); err != nil {
			return nil, fmt.Errorf("failed to parse batch results: %w", err)
		}
	}

	return &batchResponse, nil
}

// validateRules checks if suggested rules exist in the database
func validateRules(suggestedRules []string, availableRules []string) []string {
	validRules := make([]string, 0, len(suggestedRules))
	for _, rule := range suggestedRules {
		if sliceContains(availableRules, rule) {
			validRules = append(validRules, rule)
		}
	}
	return validRules
}

// sliceContains is a helper function to check if a slice contains a string
func sliceContains(slice []string, item string) bool {
	for _, v := range slice {
		if v == item {
			return true
		}
	}
	return false
}

func (s *DocumentService) CheckRuleCompliance(ocrText, ruleName, rulePattern string) (map[string]interface{}, error) {
	// Rate limit the compliance check
	if !ruleRateLimiter.Allow("rule_compliance_check") {
		return nil, fmt.Errorf("rate limit exceeded for rule compliance check")
	}

	// Validate input parameters
	if ocrText == "" {
		return nil, fmt.Errorf("empty OCR text provided")
	}
	if ruleName == "" {
		return nil, fmt.Errorf("empty rule name provided")
	}

	// Custom pattern matching for specific rules
	var complianceCheck bool
	log.Printf("COMPLIANCE DEBUG - Processing Rule: '%s', Rule Pattern: '%s'", ruleName, rulePattern)

	switch ruleName {
	case "Confidentiality Marking":
		// Detailed logging and multiple check methods
		log.Printf("COMPLIANCE DEBUG - Initial OCR Text: '%s'", ocrText)

		// Check exact match
		exactMatch := strings.Contains(ocrText, "Confidential")

		// Case-insensitive match
		caseInsensitiveMatch := strings.Contains(strings.ToLower(ocrText), strings.ToLower("Confidential"))

		// Case-insensitive regex match
		regexCaseInsensitiveMatch, _ := regexp.MatchString(`(?i)confidential`, ocrText)

		// Regex match with original pattern
		regexMatch, _ := regexp.MatchString(`Confidential`, ocrText)

		// Determine compliance check
		complianceCheck = exactMatch || caseInsensitiveMatch || regexCaseInsensitiveMatch || regexMatch

		log.Printf("COMPLIANCE DEBUG - Confidentiality Marking Rule Checks: "+
			"Exact Match='%v', "+
			"Case-Insensitive String Match='%v', "+
			"Case-Insensitive Regex Match='%v', "+
			"Regex Match='%v', "+
			"Final Compliance Check=%v",
			exactMatch,
			caseInsensitiveMatch,
			regexCaseInsensitiveMatch,
			regexMatch,
			complianceCheck)
	case "Signature Requirement":
		log.Printf("COMPLIANCE DEBUG - Processing Signature Requirement Rule")
		complianceCheck, _ = regexp.MatchString(`(?i)(signature|signed).*date`, ocrText)
		log.Printf("COMPLIANCE DEBUG - Signature Requirement Check: %v", complianceCheck)
	default:
		log.Printf("COMPLIANCE DEBUG - Using Default Rule Pattern Matching")
		complianceCheck, _ = regexp.MatchString(rulePattern, ocrText)
		log.Printf("COMPLIANCE DEBUG - Default Rule Check: %v", complianceCheck)
	}

	log.Printf("COMPLIANCE DEBUG - Final Compliance Check for Rule '%s': %v", ruleName, complianceCheck)

	// Prepare Groq API request payload
	requestPayload := struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		Model       string  `json:"model"`
		Temperature float64 `json:"temperature"`
	}{
		Messages: []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{
			{
				Role:    "system",
				Content: "You are an advanced compliance rule analyzer with expertise in legal document validation.",
			},
			{
				Role: "user",
				Content: fmt.Sprintf(`Analyze the document for compliance with the rule '%s':

Rule Name: %s
Rule Pattern: %s
Initial Compliance Check: %v

Document Text:
%s`, ruleName, ruleName, rulePattern, complianceCheck, ocrText),
			},
		},
		Model:       "mixtral-8x7b-32768",
		Temperature: 0.8,
	}

	// Serialize payload
	payloadBytes, err := json.Marshal(requestPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize request payload: %w", err)
	}

	// Create HTTP request with context and timeout
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.groq.com/openai/v1/chat/completions", bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create Groq API request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", os.Getenv("GROQ_API_KEY")))
	req.Header.Set("Content-Type", "application/json")

	// Use a custom HTTP client with timeout
	client := &http.Client{
		Timeout: 45 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			IdleConnTimeout:     45 * time.Second,
			DisableCompression:  true,
			TLSHandshakeTimeout: 15 * time.Second,
		},
	}

	// Execute request with retries
	var resp *http.Response
	for attempt := 0; attempt < 3; attempt++ {
		resp, err = client.Do(req)
		if err == nil {
			break
		}
		log.Printf("Groq API request attempt %d failed: %v", attempt+1, err)
		time.Sleep(time.Duration(attempt+1) * time.Second) // Exponential backoff
	}

	if err != nil {
		return nil, fmt.Errorf("failed to send Groq API request after 3 attempts: %w", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("non-200 status code: %d, response: %s", resp.StatusCode, string(body))
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read Groq response: %w", err)
	}

	// Parse Groq API response
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse Groq response structure: %w", err)
	}

	// Validate response content
	if len(result.Choices) == 0 || result.Choices[0].Message.Content == "" {
		return nil, fmt.Errorf("no compliance analysis returned from Groq")
	}

	// Parse compliance response
	var complianceResponse map[string]interface{}
	if err := json.Unmarshal([]byte(result.Choices[0].Message.Content), &complianceResponse); err != nil {
		return nil, fmt.Errorf("failed to parse compliance response JSON: %w", err)
	}

	// Normalize status for backward compatibility
	status, _ := complianceResponse["status"].(string)
	switch status {
	case "partial_pass", "soft_fail":
		complianceResponse["status"] = "fail"
	case "pass":
		// Keep as is
	default:
		complianceResponse["status"] = "fail"
	}

	// Enrich response with rule name
	complianceResponse["rule_name"] = ruleName

	// Log the result with more context
	confidenceScore, _ := complianceResponse["confidence_score"].(float64)
	log.Printf("Detailed Compliance Check for Rule '%s': Status=%s, Confidence=%.2f%%",
		ruleName,
		complianceResponse["status"],
		confidenceScore)

	// Log non-compliance details if available
	if nonComplianceDetails, ok := complianceResponse["non_compliance_details"].(map[string]interface{}); ok {
		for ruleName, details := range nonComplianceDetails {
			log.Printf("Non-Compliance Details for %s: %+v", ruleName, details)
		}
	}

	return complianceResponse, nil
}

// GetAllComplianceRules retrieves all compliance rules from the database
func (s *DocumentService) GetAllComplianceRules() ([]model.ComplianceRule, error) {
	// Rate limit rule retrieval
	if !ruleRateLimiter.Allow("rule_retrieval") {
		return nil, fmt.Errorf("rate limit exceeded for rule retrieval")
	}

	var rules []model.ComplianceRule
	result := s.db.Find(&rules)
	if result.Error != nil {
		log.Printf("ERROR fetching compliance rules: %v", result.Error)
		return nil, result.Error
	}

	log.Printf("Retrieved %d compliance rules from database", len(rules))
	return rules, nil
}

// GetComplianceRulesByNames retrieves specific compliance rules by their names
func (s *DocumentService) GetComplianceRulesByNames(ruleNames []string) ([]model.ComplianceRule, error) {
	// Rate limit rule retrieval by names
	if !ruleRateLimiter.Allow("rule_retrieval_by_names") {
		return nil, fmt.Errorf("rate limit exceeded for rule retrieval by names")
	}

	var rules []model.ComplianceRule
	result := s.db.Where("name IN ?", ruleNames).Find(&rules)
	if result.Error != nil {
		log.Printf("ERROR fetching compliance rules by names: %v", result.Error)
		return nil, result.Error
	}

	log.Printf("Retrieved %d compliance rules for names: %v", len(rules), ruleNames)
	return rules, nil
}

// CalculateRiskScore computes a score based on failed rules and their severity
func (s *DocumentService) CalculateRiskScore(results []map[string]interface{}, rules []model.ComplianceRule) float64 {
	// Rate limit risk score calculation
	if !ruleRateLimiter.Allow("risk_score_calculation") {
		return 0.0
	}

	log.Printf("Calculating Risk Score. Number of results: %d", len(results))

	severityWeights := map[string]float64{
		"high":   3.0,
		"medium": 2.0,
		"low":    1.0,
	}
	riskScore := 0.0

	// Create a map of rules for easier lookup
	ruleMap := make(map[string]model.ComplianceRule)
	for _, rule := range rules {
		ruleMap[rule.Name] = rule
	}

	for i, result := range results {
		log.Printf("Processing result %d: %+v", i, result)

		status, ok := result["status"].(string)
		if !ok {
			log.Printf("WARNING: Could not extract status from result %d", i)
			continue
		}

		// Get the rule name from the result
		ruleName, ok := result["rule_name"].(string)
		if !ok {
			log.Printf("WARNING: Could not extract rule_name from result %d", i)
			// Fallback to using the index if available
			if i < len(rules) {
				ruleName = rules[i].Name
			} else {
				continue
			}
		}

		if status == "fail" {
			rule, exists := ruleMap[ruleName]
			if exists {
				ruleSeverity := rule.Severity
				log.Printf("Failed rule %s with severity: %s", ruleName, ruleSeverity)

				weight, exists := severityWeights[ruleSeverity]
				if !exists {
					log.Printf("WARNING: Unknown severity level: %s", ruleSeverity)
					weight = 1.0 // Default to low risk
				}

				riskScore += weight
				log.Printf("Updated risk score: %f", riskScore)
			} else {
				log.Printf("WARNING: Rule '%s' not found in rule map", ruleName)
			}
		}
	}

	log.Printf("Final Risk Score: %f", riskScore)
	return riskScore
}

type BatchComplianceRequest struct {
	Documents []DocumentComplianceCheck `json:"documents"`
	RuleNames []string                  `json:"rule_names"` // Keep ruleNames in scope
}

type DocumentComplianceCheck struct {
	ID      string `json:"id"`
	OCRText string `json:"ocr_text"`
}

type BatchComplianceResponse struct {
	Results map[string][]string `json:"results"`
}

// Helper function to get minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// func validateRules(suggestedRules []string, availableRules []string) []string {
//     validRules := make([]string, 0, len(suggestedRules))
//     for _, rule := range suggestedRules {
//         if contains(availableRules, rule) {
//             validRules = append(validRules, rule)
//         }
//     }
//     return validRules
// }
