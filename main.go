package main

import (
	// "yourproject/controllers"
	// "yourproject/services"
	"log"
	"net/http"

	controller "github.com/Itish41/LegalEagle/controller"
	"github.com/Itish41/LegalEagle/initializers"
	middleware "github.com/Itish41/LegalEagle/middleware"
	service "github.com/Itish41/LegalEagle/service"

	"github.com/gin-gonic/gin"
)

func init() {
	// if err := initializers.LoadEnv(); err != nil {
	// 	log.Fatalf("[CRITICAL] Failed to load env: %s", err)
	// }
	if err := initializers.ConnectDB(); err != nil {
		log.Fatalf("[CRITICAL] Failed to initialize database connection: %s", err)
	}
	// Uncomment to run migrations
	if err := initializers.Migrate(); err != nil {
		log.Fatalf("[CRITICAL] Failed to run database migrations: %s", err)
	}
}

func main() {
	docService, err := service.NewDocumentService(initializers.DB)
	if err != nil {
		log.Fatalf("Failed to initialize document service: %s", err)
	}

	docController := controller.NewDocumentController(docService)

	router := gin.Default()
	router.Use(middleware.CORSMiddleware())

	// Global rate limiter for most routes
	router.Use(middleware.GlobalRateLimiter.Limit())

	// Sensitive routes with stricter rate limiting
	router.POST("/upload",
		middleware.StrictRateLimiter.Limit(),
		docController.UploadDocument)

	// Compliance rules endpoints with strict rate limiting
	router.POST("/rules",
		middleware.StrictRateLimiter.Limit(),
		docController.AddComplianceRule)

	router.GET("/rules", docController.GetAllComplianceRules)
	router.POST("/rules/by-names", docController.GetComplianceRulesByNames)

	// Healthcheck endpoint
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "healthy"})
	})

	router.POST("/action-update/:id", docController.AssignActionItem)
	// Other endpoints
	router.GET("/search", docController.SearchDocuments)
	router.GET("/dashboard", docController.GetAllDocuments)
	router.GET("/action-items", docController.GetPendingActionItemsWithTitles)
	router.PUT("/action-items/:id/complete",
		middleware.StrictRateLimiter.Limit(),
		docController.CompleteActionItem)

	router.Run(":8080")
}
