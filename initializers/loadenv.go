package initializers

import (
	"fmt"
	"log"

	"github.com/joho/godotenv"
)

func LoadEnv() error {
	log.Println("Loading env file")
	err := godotenv.Load() // using the joho library to load variables from the .env file
	if err != nil {
		log.Println("env not loading")
		return fmt.Errorf("env not loading")
	}
	log.Println("Env loaded successfully")
	return nil
}
