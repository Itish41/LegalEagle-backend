package services

import (
	"encoding/json"
	"fmt"
	"log"
	"net/smtp"
	"os"
	"regexp"
	"strings"
	"time"

	model "github.com/Itish41/LegalEagle/models"
	"gorm.io/datatypes"
)

// CreateActionItems generates action items for failed compliance rules
func (s *DocumentService) CreateActionItems(doc model.Document) error {
	var results []map[string]interface{}
	if err := json.Unmarshal([]byte(doc.ParsedData), &results); err != nil {
		log.Printf("Error unmarshaling parsed_data: %v", err)
		return err
	}

	for _, result := range results {
		status, ok := result["status"].(string)
		if !ok || status != "fail" {
			continue // Skip non-failed rules
		}

		ruleName, ok := result["rule_name"].(string)
		if !ok {
			log.Printf("Missing rule_name in compliance result: %+v", result)
			continue
		}
		log.Printf("Processing failed rule: %s", ruleName)

		var rule model.ComplianceRule
		if err := s.db.Where("name = ?", ruleName).First(&rule).Error; err != nil {
			log.Printf("Rule %s not found in compliance_rules: %v", ruleName, err)
			continue
		}

		if rule.ID == "" {
			log.Printf("Invalid RuleID for %s; skipping action item creation", ruleName)
			continue
		}

		explanation, _ := result["explanation"].(string)
		severity, _ := result["severity"].(string)
		action := model.ActionItem{
			DocumentID:  doc.ID,
			RuleID:      rule.ID,
			Description: fmt.Sprintf("Address %s non-compliance: %s", ruleName, explanation),
			Priority:    strings.Title(strings.ToLower(severity)), // Use severity from parsed_data
			Status:      "pending",
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
			// AssignedTo is intentionally left empty
			DueDate: time.Now().AddDate(0, 1, 0), // Default due date: 1 month from now
		}

		// Use Omit to skip the AssignedTo field
		if err := s.db.Omit("AssignedTo").Create(&action).Error; err != nil {
			log.Printf("Error creating action item: %v", err)
			return err
		}
		log.Printf("Action item created: %s for document %s", action.Description, doc.ID)

		docResult := model.DocumentRuleResult{
			DocumentID: doc.ID,
			RuleID:     rule.ID,
			Status:     "fail",
			Details:    datatypes.JSON(marshalResult(result)),
			CreatedAt:  time.Now(),
		}
		if err := s.db.Create(&docResult).Error; err != nil {
			log.Printf("Error creating document rule result: %v", err)
			return err
		}
		log.Printf("Document rule result created for rule %s, document %s", ruleName, doc.ID)
	}
	return nil
}

// Helper to marshal result into JSON bytes
func marshalResult(result map[string]interface{}) []byte {
	bytes, err := json.Marshal(result)
	if err != nil {
		log.Printf("[marshalResult] Error marshaling result: %v", err)
		return []byte("{}")
	}
	return bytes
}

// AssignAndNotifyActionItem updates the AssignedTo field of an action item and sends an email notification using Gmail SMTP.
func (s *DocumentService) AssignAndNotifyActionItem(actionID string, email string) error {
	// Retrieve the action item from the database.
	var action model.ActionItem
	if err := s.db.First(&action, "id = ?", actionID).Error; err != nil {
		log.Printf("[AssignAndNotifyActionItem] Error fetching action item %s: %v", actionID, err)
		return err
	}

	// Update the AssignedTo field.
	action.AssignedTo = email
	action.UpdatedAt = time.Now()
	if err := s.db.Model(&action).Update("AssignedTo", email).Error; err != nil {
		log.Printf("[AssignAndNotifyActionItem] Error updating AssignedTo for action item %s: %v", actionID, err)
		return err
	}
	log.Printf("[AssignAndNotifyActionItem] Updated AssignedTo to %s for action item %s", email, actionID)

	passWord := os.Getenv("GMAIL_PASSWORD")
	// Gmail SMTP configuration.
	// Replace these with environment variables or secure config values in production.
	from := "itish.srivastava@think41.com" // your Gmail address
	password := passWord                   // your Gmail app-specific password
	smtpHost := "smtp.gmail.com"
	smtpPort := "587"

	/// Prepare the email content.
	subject := fmt.Sprintf("Action Item Assigned: %s", action.Description)
	body := fmt.Sprintf(`
	<html>
	<body>
		<h2>Action Item Assigned</h2>
		<p>Dear User,</p>
		<p>You have been assigned a new action item:</p>
		<ul>
			<li><strong>Title:</strong> %s</li>
			<li><strong>Description:</strong> %s</li>
			<li><strong>Due Date:</strong> %s</li>
			<li><strong>Priority:</strong> %s</li>
		</ul>
		<p>Please take the necessary actions to complete it.</p>
		<p>Best regards,<br>Your Team</p>
	</body>
	</html>
`, "Action Item Assigned", action.Description, action.DueDate.Format("January 2, 2006"), action.Priority)
	// Construct the email message.
	message := []byte("Subject: " + subject + "\r\n" +
		"From: " + from + "\r\n" +
		"To: " + email + "\r\n" +
		"Content-Type: text/html; charset=UTF-8\r\n\r\n" +
		body)

	// Set up authentication.
	auth := smtp.PlainAuth("", from, password, smtpHost)

	// Send the email.
	err := smtp.SendMail(smtpHost+":"+smtpPort, auth, from, []string{email}, message)
	if err != nil {
		log.Printf("[AssignAndNotifyActionItem] Error sending email for action item %s: %v", actionID, err)
		return err
	}
	log.Printf("[AssignAndNotifyActionItem] Email sent successfully to %s for action item %s", email, actionID)
	return nil
}

// GetPendingActionItemsWithTitles retrieves pending action items with document titles
func (s *DocumentService) GetPendingActionItemsWithTitles() ([]map[string]interface{}, error) {
	var items []model.ActionItem
	if err := s.db.Where("status = ?", "pending").Find(&items).Error; err != nil {
		log.Printf("[GetPendingActionItemsWithTitles] Error fetching pending action items: %v", err)
		return nil, err
	}

	result := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		var doc model.Document
		if err := s.db.Select("title").Where("id = ?", item.DocumentID).First(&doc).Error; err != nil {
			log.Printf("[GetPendingActionItemsWithTitles] Error fetching document title for %s: %v", item.DocumentID, err)
			continue
		}
		result = append(result, map[string]interface{}{
			"id":          item.ID,
			"document_id": item.DocumentID,
			"title":       doc.Title,
			"rule_id":     item.RuleID,
			"description": item.Description,
			"priority":    item.Priority,
			"assigned_to": item.AssignedTo,
			"due_date":    item.DueDate,
			"status":      item.Status,
		})
	}
	return result, nil
}

// UpdateActionItem marks an action as completed and updates DocumentRuleResult
func (s *DocumentService) UpdateActionItem(actionID string) error {
	var action model.ActionItem
	if err := s.db.First(&action, "id = ?", actionID).Error; err != nil {
		log.Printf("[UpdateActionItem] Error fetching action item %s: %v", actionID, err)
		return err
	}

	action.Status = "completed"
	action.UpdatedAt = time.Now()

	// Use Omit to skip the AssignedTo field to avoid UUID validation error
	if err := s.db.Model(&action).Omit("AssignedTo").Updates(map[string]interface{}{
		"Status":    "completed",
		"UpdatedAt": time.Now(),
	}).Error; err != nil {
		log.Printf("[UpdateActionItem] Error updating action item %s: %v", actionID, err)
		return err
	}

	var docResult model.DocumentRuleResult
	if err := s.db.Where("document_id = ? AND rule_id = ?", action.DocumentID, action.RuleID).First(&docResult).Error; err != nil {
		log.Printf("[UpdateActionItem] Error fetching document rule result for action %s: %v", actionID, err)
		return err
	}

	// Update status to resolved and set explanation to "No issues" in Details JSON
	docResult.Status = "resolved"

	// Parse the current Details JSON
	details := make(map[string]interface{})
	if len(docResult.Details) > 0 {
		if err := json.Unmarshal(docResult.Details, &details); err != nil {
			log.Printf("[UpdateActionItem] Error unmarshaling Details JSON: %v", err)
			// Create a new map if unmarshaling fails
			details = make(map[string]interface{})
		}
	}

	// Update the explanation
	details["explanation"] = "No issues"

	// Marshal back to JSON
	updatedDetails, err := json.Marshal(details)
	if err != nil {
		log.Printf("[UpdateActionItem] Error marshaling updated Details: %v", err)
		return err
	}

	docResult.Details = datatypes.JSON(updatedDetails)
	docResult.CreatedAt = time.Now() // Consider adding UpdatedAt to the model

	if err := s.db.Save(&docResult).Error; err != nil {
		log.Printf("[UpdateActionItem] Error updating document rule result for action %s: %v", actionID, err)
		return err
	}

	// Update the document's parsed_data field to set status to true
	var doc model.Document
	if err := s.db.First(&doc, "id = ?", action.DocumentID).Error; err != nil {
		log.Printf("[UpdateActionItem] Error fetching document %s: %v", action.DocumentID, err)
		return err
	}

	// Get current parsed data as map
	parsedData := make(map[string]interface{})
	if len(doc.ParsedData) > 0 {
		if err := json.Unmarshal(doc.ParsedData, &parsedData); err != nil {
			log.Printf("[UpdateActionItem] Error unmarshaling parsed data for document %s: %v", action.DocumentID, err)
			// Continue even if there's an error with the parsed data
			parsedData = make(map[string]interface{})
		}
	}

	// Update status to true
	parsedData["status"] = true

	// Marshal back to JSON
	updatedParsedData, err := json.Marshal(parsedData)
	if err != nil {
		log.Printf("[UpdateActionItem] Error marshaling updated parsed data for document %s: %v", action.DocumentID, err)
		return err
	}

	// Update the document
	if err := s.db.Model(&doc).Updates(map[string]interface{}{
		"ParsedData": updatedParsedData,
		"UpdatedAt":  time.Now(),
	}).Error; err != nil {
		log.Printf("[UpdateActionItem] Error updating document %s parsed data: %v", action.DocumentID, err)
		return err
	}

	log.Printf("[UpdateActionItem] Successfully updated document %s parsed_data status to true", action.DocumentID)
	return nil
}

// GetPendingActionItems retrieves all pending action items
func (s *DocumentService) GetPendingActionItems() ([]model.ActionItem, error) {
	var items []model.ActionItem
	if err := s.db.Where("status = ?", "pending").Find(&items).Error; err != nil {
		log.Printf("[GetPendingActionItems] Error fetching pending action items: %v", err)
		return nil, err
	}
	return items, nil
}

// Helper functions
func extractRuleName(explanation string) string {
	// Convert to lowercase for consistent matching
	explanation = strings.ToLower(explanation)

	// Specific handling for NDA Check rule
	if strings.Contains(explanation, "non-disclosure agreement") {
		return "NDA Check"
	}

	// Predefined rule mappings
	ruleMap := map[string]string{
		"nda check":          "NDA Check",
		"confidentiality":    "Confidentiality Check",
		"document integrity": "Document Integrity Check",
	}

	// Check for predefined rules first
	for keyword, ruleName := range ruleMap {
		if strings.Contains(explanation, keyword) {
			return ruleName
		}
	}

	// Extract rule name from quotes or specific patterns
	patterns := []string{
		"'([^']*)'",       // Extract text between single quotes
		"\"([^\"]*)\"",    // Extract text between double quotes
		"rule\\s*([^:]+)", // Extract text after "rule"
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(explanation)
		if len(matches) > 1 {
			ruleName := strings.TrimSpace(matches[1])
			if ruleName != "" {
				return ruleName
			}
		}
	}

	// Fallback extraction methods
	if strings.Contains(explanation, "required by") {
		parts := strings.Split(explanation, "required by")
		if len(parts) > 1 {
			ruleName := strings.TrimSpace(parts[1])
			if ruleName != "" {
				return ruleName
			}
		}
	}

	// Final fallback
	log.Printf("Could not extract rule name from explanation: %s", explanation)
	return "Unknown Rule"
}
