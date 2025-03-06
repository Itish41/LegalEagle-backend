package initializers

import (
	"fmt"
	"log"
	"os"

	_ "github.com/lib/pq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	// "database/sql"
)

var DB *gorm.DB // automigrate is also using this var

func ConnectDB() error {
	log.Println("Connecting to database")

	dsn := os.Getenv("DIRECT_URL")
	if dsn == "" {
		log.Println("DIRECT_URL variable not loading...")
		return fmt.Errorf("env variable DIRECT_URL is empty")
	}

	var err error
	// Configure Postgres driver
	pgConfig := postgres.Config{
		PreferSimpleProtocol: true, // Disable implicit prepared statement usage
		DriverName:           "postgres",
		DSN:                  dsn,
	}

	// Configure GORM
	DB, err = gorm.Open(postgres.New(pgConfig), &gorm.Config{
		PrepareStmt:          false,
		DisableAutomaticPing: true,
	})
	if err != nil {
		return fmt.Errorf("failed to connect to the database: %w", err)
	}

	// Optionally, use Debug() to log SQL queries during development.
	DB = DB.Debug()

	log.Println("Database connection successful")
	return nil
}
