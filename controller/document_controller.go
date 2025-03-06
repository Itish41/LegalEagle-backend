package controller

import (
	"log"
	"net/http"

	service "github.com/Itish41/LegalEagle/service"

	"github.com/gin-gonic/gin"
)

// DocumentController manages HTTP requests for document uploads
// type DocumentController struct {
// 	service *service.DocumentService
// }

// NewDocumentController initializes the controller with the service
func NewDocumentController(service *service.DocumentService) *DocumentController {
	return &DocumentController{service}
}

// UploadDocument handles the file upload request
func (c *DocumentController) UploadDocument(ctx *gin.Context) {
	file, header, err := ctx.Request.FormFile("file")
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Failed to get file from request"})
		return
	}
	defer file.Close()

	ocrText, fileID, fileURL, complianceResults, riskScore, err := c.service.UploadAndProcessDocument(file, header) // Update service to return these
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, gin.H{
		"message":           "Document uploaded and processed successfully",
		"ocrText":           ocrText,
		"fileID":            fileID,
		"fileURL":           fileURL,
		"complianceResults": complianceResults, // Optional
		"riskScore":         riskScore,
	})
}

// GetAllDocuments retrieves all documents from the database
func (dc *DocumentController) GetAllDocuments(c *gin.Context) {
	log.Println("DocumentController: Fetching all documents")

	docs, err := dc.service.GetAllDocuments()
	if err != nil {
		log.Printf("Error fetching documents: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to retrieve documents",
			"details": err.Error(),
		})
		return
	}

	// Log first few documents for debugging
	for i, doc := range docs {
		if i < 3 {
			log.Printf("document %d - ID: %v, Title: %s, OCR Text Length: %d, Risk Score: %f",
				i+1, doc["id"], doc["title"], len(doc["ocr_text"].(string)), doc["risk_score"])
		}
	}

	// Return documents with additional metadata
	c.JSON(http.StatusOK, gin.H{
		"documents": docs,
		"total":     len(docs),
	})
}

// In controllers
func (c *DocumentController) SearchDocuments(ctx *gin.Context) {
	query := ctx.Query("q")
	if query == "" {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": "Query parameter 'q' is required"})
		return
	}

	results, err := c.service.SearchDocuments(query)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"message": "Search completed successfully",
		"results": results,
	})
}
