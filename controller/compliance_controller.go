package controller

import (
	"net/http"

	"github.com/Itish41/LegalEagle/models"
	service "github.com/Itish41/LegalEagle/service"
	"github.com/gin-gonic/gin"
)

// DocumentController manages HTTP requests for document uploads
type DocumentController struct {
	service *service.DocumentService
}

func (c *DocumentController) AddComplianceRule(ctx *gin.Context) {
	var rule models.ComplianceRule
	if err := ctx.ShouldBindJSON(&rule); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := c.service.AddComplianceRule(&rule); err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusCreated, rule)
}

// GetAllComplianceRules retrieves all compliance rules from the database
func (c *DocumentController) GetAllComplianceRules(ctx *gin.Context) {
	rules, err := c.service.GetAllComplianceRules()
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	ctx.JSON(http.StatusOK, rules)
}

// GetComplianceRulesByNames retrieves compliance rules by their names
func (c *DocumentController) GetComplianceRulesByNames(ctx *gin.Context) {
	var request struct {
		Names []string `json:"names" binding:"required"`
	}

	if err := ctx.ShouldBindJSON(&request); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	rules, err := c.service.GetComplianceRulesByNames(request.Names)
	if err != nil {
		ctx.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	ctx.JSON(http.StatusOK, rules)
}
