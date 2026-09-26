package utils

import (
	"fmt"
	"log"

	"github.com/xshubhamg/file-upload/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var DB *gorm.DB

func SetupDB() {
	fmt.Println("Connecting to DB...")

	var err error

	DB, err = gorm.Open(sqlite.Open("db.sqlite"), &gorm.Config{})
	if err != nil {
		log.Fatalln("Failed to open database!")
	}

	if err := DB.AutoMigrate(&models.User{}, &models.File{}); err != nil {
		log.Fatalln("Failed to migrate database!")
	}

	fmt.Println("Connected to db successfully!")
}
