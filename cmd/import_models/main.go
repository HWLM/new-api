package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type modelRow struct {
	ID           int    `json:"id"`
	ModelName    string `json:"model_name"`
	Description  string `json:"description"`
	Icon         string `json:"icon"`
	Tags         string `json:"tags"`
	VendorID     int    `json:"vendor_id"`
	Endpoints    string `json:"endpoints"`
	Status       int    `json:"status"`
	SyncOfficial int    `json:"sync_official"`
	CreatedTime  int64  `json:"created_time"`
	UpdatedTime  int64  `json:"updated_time"`
	NameRule     int    `json:"name_rule"`
}

func (modelRow) TableName() string { return "models" }

type payload struct {
	Data struct {
		Items []modelRow `json:"items"`
	} `json:"data"`
}

func main() {
	dataPath := flag.String("data", "data.json", "path to data.json")
	envPath := flag.String("env", ".env", "path to .env")
	flag.Parse()

	if err := godotenv.Load(*envPath); err != nil {
		log.Printf("warn: load %s: %v (continuing with process env)", *envPath, err)
	}

	dsn := strings.TrimSpace(os.Getenv("SQL_DSN"))
	if dsn == "" {
		log.Fatalf("SQL_DSN not set")
	}
	if !strings.HasPrefix(dsn, "postgres://") && !strings.HasPrefix(dsn, "postgresql://") {
		log.Fatalf("this importer expects a PostgreSQL DSN; got: %s", dsn)
	}

	raw, err := os.ReadFile(*dataPath)
	if err != nil {
		log.Fatalf("read %s: %v", *dataPath, err)
	}
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		log.Fatalf("parse %s: %v", *dataPath, err)
	}
	if len(p.Data.Items) == 0 {
		log.Fatalf("no items in %s", *dataPath)
	}
	log.Printf("parsed %d items from %s", len(p.Data.Items), *dataPath)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("open db: %v", err)
	}

	tx := db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"model_name", "description", "icon", "tags", "vendor_id",
			"endpoints", "status", "sync_official", "created_time",
			"updated_time", "name_rule",
		}),
	}).Create(&p.Data.Items)
	if err := tx.Error; err != nil {
		log.Fatalf("upsert: %v", err)
	}
	log.Printf("upserted rows affected=%d", tx.RowsAffected)

	if err := db.Exec(`SELECT setval(pg_get_serial_sequence('models','id'), (SELECT MAX(id) FROM models))`).Error; err != nil {
		log.Printf("warn: reset sequence: %v", err)
	} else {
		log.Printf("reset models_id_seq to MAX(id)")
	}

	fmt.Println("done")
}
