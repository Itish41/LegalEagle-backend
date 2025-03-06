package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	model "github.com/Itish41/LegalEagle/models"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/elastic/go-elasticsearch/v8"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// DocumentService handles document processing logic
type DocumentService struct {
	s3Client *s3.S3
	esClient *elasticsearch.Client
	db       *gorm.DB
}

// NewDocumentService initializes the service with an S3 client and Elasticsearch client
func NewDocumentService(db *gorm.DB) (*DocumentService, error) {
	region := os.Getenv("SUPABASE_REGION")
	endpoint := os.Getenv("SUPABASE_S3_ENDPOINT")
	accessKey := os.Getenv("SUPABASE_ACCESS_KEY")
	secretKey := os.Getenv("SUPABASE_SECRET_KEY")

	if region == "" || endpoint == "" || accessKey == "" || secretKey == "" {
		return nil, fmt.Errorf("missing required S3 configuration environment variables")
	}

	sess, err := session.NewSession(&aws.Config{
		Region:           aws.String(region),
		Endpoint:         aws.String(endpoint),
		DisableSSL:       aws.Bool(false), // Changed to false for most cloud providers
		Credentials:      credentials.NewStaticCredentials(accessKey, secretKey, ""),
		S3ForcePathStyle: aws.Bool(true),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %w", err)
	}

	// Initialize Elasticsearch client
	esURL := os.Getenv("ELASTICSEARCH_URL")
	var esClient *elasticsearch.Client
	if esURL != "" {
		esConfig := elasticsearch.Config{
			Addresses: []string{esURL},
		}
		var err error
		esClient, err = elasticsearch.NewClient(esConfig)
		if err != nil {
			log.Printf("Warning: Failed to create Elasticsearch client: %v", err)
		}
	}

	return &DocumentService{s3Client: s3.New(sess), esClient: esClient, db: db}, nil
}

// UploadAndProcessDocument uploads the file to Supabase S3 and processes it with OCR.space
func (s *DocumentService) UploadAndProcessDocument(file multipart.File, header *multipart.FileHeader) (string, string, string, string, float64, error) {
	log.Println("Starting UploadAndProcessDocument")
	log.Printf("File details: Name=%s, Size=%d", header.Filename, header.Size)

	// Step 1: Upload file to Supabase S3
	fileBytes, err := io.ReadAll(file)
	if err != nil {
		log.Printf("ERROR reading file: %v", err)
		return "", "", "", "", 0.0, fmt.Errorf("failed to read file: %w", err)
	}

	fileID := fmt.Sprintf("%d-%s", time.Now().Unix(), header.Filename)
	bucket := os.Getenv("SUPABASE_BUCKET")
	if bucket == "" {
		log.Println("SUPABASE_BUCKET environment variable is not set")
		return "", "", "", "", 0.0, fmt.Errorf("bucket name not configured")
	}

	uploadInput := &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(fileID),
		Body:        bytes.NewReader(fileBytes),
		ACL:         aws.String("public-read"),
		ContentType: aws.String(header.Header.Get("Content-Type")),
	}

	_, err = s.s3Client.PutObject(uploadInput)
	if err != nil {
		log.Printf("S3 upload error: %v", err)
		return "", "", "", "", 0.0, fmt.Errorf("failed to upload file to S3: %w", err)
	}

	fileURL := fmt.Sprintf("%s/object/public/%s/%s", os.Getenv("SUPABASE_S3_URL"), bucket, fileID)
	log.Printf("File stored at: %s", fileURL)

	// Step 2: Process with OCR.space
	apiKey := os.Getenv("OCR_SPACE_API_KEY")
	if apiKey == "" {
		log.Println("OCR_SPACE_API_KEY environment variable is not set")
		return "", "", "", "", 0.0, fmt.Errorf("OCR API key not configured")
	}

	ocrText, err := processWithOCRSpace(fileBytes, header.Filename)
	if err != nil {
		log.Printf("ERROR in OCR processing: %v", err)
		return "", "", "", "", 0.0, fmt.Errorf("failed to process OCR with OCR.space: %w", err)
	}
	log.Printf("OCR Text extracted: %s", ocrText)

	// Step 3: Index in Elasticsearch
	err = s.indexDocument(fileID, fileURL, ocrText)
	if err != nil {
		log.Printf("Elasticsearch indexing error: %v", err)
		return "", "", "", "", 0.0, fmt.Errorf("failed Merkel to index document in Elasticsearch: %w", err)
	}
	log.Printf("Document indexed successfully with ID: %s", fileID)

	// Step 4: Compliance Analysis
	// Determine violated rules using Groq
	violatedRuleNames, err := s.DetermineApplicableRules(ocrText)
	if err != nil {
		log.Printf("ERROR determining violated rules: %v", err)
		return "", "", "", "", 0.0, err
	}
	log.Printf("Violated Rules: %v", violatedRuleNames)

	// Fetch all rules to build complete parsed_data
	allRules, err := s.GetAllComplianceRules()
	if err != nil {
		log.Printf("ERROR fetching all rules from database: %v", err)
		return "", "", "", "", 0.0, fmt.Errorf("failed to fetch rules from database: %w", err)
	}
	log.Printf("Fetched %d rules from database", len(allRules))

	// Generate parsed_data for all rules
	var complianceResults []map[string]interface{}
	ruleMap := make(map[string]model.ComplianceRule)
	for _, rule := range allRules {
		ruleMap[rule.Name] = rule
		result := map[string]interface{}{
			"rule_name":   rule.Name,
			"severity":    rule.Severity,
			"status":      "pass",
			"explanation": fmt.Sprintf("The document complies with the '%s' rule.", rule.Name),
		}
		if contains(violatedRuleNames, rule.Name) {
			result["status"] = "fail"
			result["explanation"] = fmt.Sprintf("The document violates the '%s' rule: does not meet the required pattern '%s'.", rule.Name, rule.Pattern)
		}
		complianceResults = append(complianceResults, result)
		log.Printf("Compliance result for %s: %+v", rule.Name, result)
	}

	// Calculate risk score
	riskScore := s.CalculateRiskScore(complianceResults, allRules)
	log.Printf("Calculated Risk Score: %f", riskScore)

	// Marshal compliance results
	parsedDataJSON, err := json.Marshal(complianceResults)
	if err != nil {
		log.Printf("ERROR marshaling compliance results: %v", err)
		return "", "", "", "", 0.0, fmt.Errorf("failed to marshal compliance results: %w", err)
	}
	log.Printf("Compliance Results JSON: %s", string(parsedDataJSON))

	// Step 5: Save to database with compliance results
	fileName := filepath.Base(fileURL)
	fileType := filepath.Ext(fileName)
	if fileType != "" {
		fileType = fileType[1:] // Remove the leading dot
	}
	title := strings.TrimSuffix(fileName, fileType)

	doc := model.Document{
		Title:       title,
		FileType:    fileType,
		OriginalURL: fileURL,
		OcrText:     ocrText,
		ParsedData:  datatypes.JSON(parsedDataJSON),
		RiskScore:   riskScore,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := s.db.Create(&doc).Error; err != nil {
		log.Printf("ERROR saving document to database: %v", err)
		return "", "", "", "", 0.0, fmt.Errorf("failed to save to database: %w", err)
	}
	log.Printf("Document saved to database successfully with ID: %s", doc.ID)

	// Step 6: Create Action Items and Document Rule Results
	err = s.CreateActionItems(doc)
	if err != nil {
		log.Printf("Error creating action items: %v", err)
		return "", "", "", "", 0.0, fmt.Errorf("failed to create action items: %w", err)
	}
	log.Printf("Action items processed for document %s", doc.ID)

	return ocrText, fileID, fileURL, string(parsedDataJSON), riskScore, nil
}

// Helper function to check if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// SearchDocuments searches for documents in Elasticsearch
func (s *DocumentService) SearchDocuments(query string) ([]map[string]interface{}, error) {
	// Validate Elasticsearch client
	if s.esClient == nil {
		return nil, fmt.Errorf("elasticsearch client is not initialized")
	}

	// Prepare the Elasticsearch query
	searchQuery := map[string]interface{}{
		"query": map[string]interface{}{
			"multi_match": map[string]interface{}{
				"query":  query,
				"fields": []string{"ocr_text", "file_id"}, // Search these fields
			},
		},
	}
	body, err := json.Marshal(searchQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal search query: %w", err)
	}

	// Execute the search
	res, err := s.esClient.Search(
		s.esClient.Search.WithContext(context.Background()),
		s.esClient.Search.WithIndex("documents"),
		s.esClient.Search.WithBody(bytes.NewReader(body)),
	)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("elasticsearch search failed: %s", res.String())
	}

	// Parse the response
	var result map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode search response: %w", err)
	}

	// Safely extract hits
	hitsMap, ok := result["hits"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid hits structure in search response")
	}

	hitsArray, ok := hitsMap["hits"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid hits array in search response")
	}

	var documents []map[string]interface{}
	for _, hit := range hitsArray {
		hitMap, ok := hit.(map[string]interface{})
		if !ok {
			continue // Skip invalid hits
		}

		source, ok := hitMap["_source"].(map[string]interface{})
		if !ok {
			continue // Skip hits without a valid source
		}

		documents = append(documents, source)
	}

	return documents, nil
}

// processWithOCRSpace sends the file to OCR.space and returns the extracted text
func processWithOCRSpace(fileBytes []byte, filename string) (string, error) {
	// Trim whitespace and validate API key
	apiKey := strings.TrimSpace(os.Getenv("OCR_SPACE_API_KEY"))
	if apiKey == "" {
		return "", fmt.Errorf("OCR.space API key is not set")
	}

	// Additional validation for API key
	if len(apiKey) < 10 {
		return "", fmt.Errorf("invalid OCR.space API key format")
	}

	// Log API key (be careful in production!)
	log.Printf("Using OCR.space API Key (first 4 chars): %s", apiKey[:4])
	log.Printf("Full API Key Length: %d", len(apiKey))

	// Determine file type based on filename extension
	fileExt := strings.ToLower(filepath.Ext(filename))
	var fileType string
	switch fileExt {
	case ".pdf":
		fileType = "PDF"
	case ".png":
		fileType = "PNG"
	case ".jpg", ".jpeg":
		fileType = "JPG"
	case ".gif":
		fileType = "GIF"
	case ".tiff", ".tif":
		fileType = "TIFF"
	default:
		fileType = "PDF" // Default to PDF if unknown
		log.Printf("Unknown file type for %s, defaulting to PDF", filename)
	}

	// Construct endpoint URL with API key
	endpoint := "https://api.ocr.space/parse/image"

	// Prepare multipart form
	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	// Add form fields
	if err := w.WriteField("apikey", apiKey); err != nil {
		return "", fmt.Errorf("failed to write apikey field: %w", err)
	}
	if err := w.WriteField("language", "eng"); err != nil {
		return "", fmt.Errorf("failed to write language field: %w", err)
	}
	if err := w.WriteField("isOverlayRequired", "false"); err != nil {
		return "", fmt.Errorf("failed to write isOverlayRequired field: %w", err)
	}
	if err := w.WriteField("filetype", fileType); err != nil {
		return "", fmt.Errorf("failed to write filetype field: %w", err)
	}

	// Add file
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}
	_, err = fw.Write(fileBytes)
	if err != nil {
		return "", fmt.Errorf("failed to write file bytes: %w", err)
	}
	w.Close()

	// Create request
	req, err := http.NewRequest("POST", endpoint, &b)
	if err != nil {
		return "", fmt.Errorf("failed to create OCR request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	// Log request details for debugging
	log.Printf("OCR Request Content-Type: %s", req.Header.Get("Content-Type"))
	log.Printf("OCR Request Endpoint: %s", endpoint)
	log.Printf("OCR File Type: %s", fileType)

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("OCR request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read the raw response body for debugging
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	// Log the raw response for debugging
	log.Printf("OCR Response Status: %s", resp.Status)
	log.Printf("OCR Response Body: %s", string(bodyBytes))

	// Try to parse the response
	var result map[string]interface{}
	err = json.Unmarshal(bodyBytes, &result)
	if err != nil {
		// If it's a plain text error message, return it as an error
		errorMsg := string(bodyBytes)
		log.Printf("OCR API Error (JSON Unmarshal): %s", errorMsg)
		return "", fmt.Errorf("OCR API error: %s", errorMsg)
	}

	// Check for error in OCR.space response
	if errorMessage, ok := result["ErrorMessage"].(string); ok && errorMessage != "" {
		log.Printf("OCR.space Error Message: %s", errorMessage)
		return "", fmt.Errorf("OCR.space error: %s", errorMessage)
	}

	// Extract parsed results
	parsedResults, ok := result["ParsedResults"].([]interface{})
	if !ok || len(parsedResults) == 0 {
		log.Println("No OCR results found in response")
		return "", fmt.Errorf("no OCR results found in response")
	}

	// Extract parsed text
	firstResult, ok := parsedResults[0].(map[string]interface{})
	if !ok {
		log.Println("Invalid parsed results format")
		return "", fmt.Errorf("invalid parsed results format")
	}

	parsedText, ok := firstResult["ParsedText"].(string)
	if !ok {
		log.Println("Failed to extract ParsedText")
		return "", fmt.Errorf("failed to extract ParsedText from OCR response")
	}

	log.Printf("OCR Text extracted successfully: %d characters", len(parsedText))
	return parsedText, nil
}

// indexDocument indexes the document in Elasticsearch
func (s *DocumentService) indexDocument(fileID, fileURL, ocrText string) error {
	// Skip indexing if Elasticsearch client is not initialized
	if s.esClient == nil {
		log.Println("Elasticsearch client not initialized. Skipping indexing.")
		return nil
	}

	doc := map[string]interface{}{
		"file_id":   fileID,
		"file_url":  fileURL,
		"ocr_text":  ocrText,
		"timestamp": time.Now().UTC(),
	}

	body, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("failed to marshal document for indexing: %w", err)
	}

	res, err := s.esClient.Index(
		"documents",
		bytes.NewReader(body),
		s.esClient.Index.WithDocumentID(fileID),
		s.esClient.Index.WithContext(context.Background()),
	)
	if err != nil {
		log.Printf("Elasticsearch indexing error: %v", err)
		return nil // Don't break the upload process
	}
	defer res.Body.Close()

	if res.IsError() {
		log.Printf("Elasticsearch indexing failed: %s", res.String())
		return nil // Don't break the upload process
	}

	log.Println("Document successfully indexed in Elasticsearch")
	return nil
}

// processDocumentCompliance processes compliance for a single document
func (s *DocumentService) processDocumentCompliance(doc model.Document) (map[string]interface{}, error) {
	// Create a map representation of the document
	docMap := map[string]interface{}{
		"id":           doc.ID,
		"title":        doc.Title,
		"file_type":    doc.FileType,
		"original_url": doc.OriginalURL,
		"ocr_text":     doc.OcrText,
		"risk_score":   doc.RiskScore,
		"parsed_data":  doc.ParsedData,
	}

	// If no OCR text, return the document map without compliance processing
	if doc.OcrText == "" {
		return docMap, nil
	}

	// Determine applicable rules (use context to cache or optimize)
	applicableRuleNames, err := s.DetermineApplicableRules(doc.OcrText)
	if err != nil || len(applicableRuleNames) == 0 {
		return docMap, err
	}

	// If no parsed data, return with rule information
	if len(doc.ParsedData) == 0 {
		docMap["applicable_rules"] = applicableRuleNames
		return docMap, nil
	}

	// Parse compliance results efficiently
	var complianceResults []map[string]interface{}
	if err := json.Unmarshal([]byte(doc.ParsedData), &complianceResults); err != nil {
		docMap["compliance_parsing_error"] = err.Error()
		return docMap, err
	}

	// Quick compliance status determination
	overallStatus := "pass"
	processedComplianceDetails := make([]map[string]interface{}, 0, len(complianceResults))

	for _, result := range complianceResults {
		// Determine the most relevant rule name
		ruleName := ""

		// First, check if the result already has a rule name
		if name, ok := result["rule_name"].(string); ok && name != "" {
			ruleName = name
		} else if name, ok := result["rule"].(string); ok && name != "" {
			ruleName = name
		}

		// If no rule name found, try to match from applicable rules
		if ruleName == "" && len(applicableRuleNames) > 0 {
			for _, name := range applicableRuleNames {
				if strings.Contains(strings.ToLower(result["explanation"].(string)), strings.ToLower(name)) {
					ruleName = name
					break
				}
			}

			// If still no match, use the first applicable rule
			if ruleName == "" {
				ruleName = applicableRuleNames[0]
			}
		}

		// Add rule name to the result
		result["rule_name"] = ruleName
		processedComplianceDetails = append(processedComplianceDetails, result)

		// Determine status efficiently
		if status, ok := result["status"].(string); !ok || status != "pass" {
			overallStatus = "fail"
		}
	}

	// Add compliance information
	docMap["compliance_status"] = overallStatus
	docMap["compliance_details"] = processedComplianceDetails
	docMap["applicable_rules"] = applicableRuleNames

	return docMap, nil
}

// GetAllDocuments retrieves all documents from the database
func (s *DocumentService) GetAllDocuments() ([]map[string]interface{}, error) {
	log.Println("GetAllDocuments: Starting database query")

	var documents []model.Document
	// Use Find with error checking
	result := s.db.Select("*").Find(&documents)

	if result.Error != nil {
		log.Printf("GetAllDocuments: Database query error: %v", result.Error)
		return nil, fmt.Errorf("failed to fetch documents: %w", result.Error)
	}

	// Check if no documents found
	if result.RowsAffected == 0 {
		log.Println("GetAllDocuments: No documents found")
		return []map[string]interface{}{}, nil
	}

	log.Printf("GetAllDocuments: Retrieved %d documents", len(documents))

	// Process documents and add compliance information
	processedDocuments := make([]map[string]interface{}, 0, len(documents))
	for _, doc := range documents {
		processedDoc, err := s.processDocumentCompliance(doc)
		if err != nil {
			log.Printf("Error processing document %s: %v", doc.ID, err)
			continue
		}
		processedDocuments = append(processedDocuments, processedDoc)
	}

	// Log processed documents summary
	log.Printf("Total Processed Documents: %d", len(processedDocuments))

	return processedDocuments, nil
}
