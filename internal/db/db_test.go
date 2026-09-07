package db

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/hellodeveye/postdare-go/internal/config"
	"github.com/hellodeveye/postdare-go/internal/model"
)

func TestOpenSQLiteCreatesFileSeedsAdminAndIsIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "postdare.db")
	var generatedPassword string

	database, err := Open(config.DatabaseConfig{Driver: "sqlite", Path: dbPath}, WithGeneratedPasswordLogger(func(password string) {
		generatedPassword = password
	}))
	if err != nil {
		t.Fatal(err)
	}
	if generatedPassword == "" {
		t.Fatal("expected generated admin password to be logged")
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatal(err)
	}
	var admin model.User
	if err := database.Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatal(err)
	}
	if !admin.MustChangePassword {
		t.Fatal("expected generated admin to require a password change")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(generatedPassword)); err != nil {
		t.Fatal("generated password does not match stored hash")
	}

	generatedPassword = ""
	second, err := Open(config.DatabaseConfig{Driver: "sqlite", Path: dbPath}, WithGeneratedPasswordLogger(func(password string) {
		generatedPassword = password
	}))
	if err != nil {
		t.Fatal(err)
	}
	if generatedPassword != "" {
		t.Fatal("second open should not generate a new admin password")
	}
	var count int64
	if err := second.Model(&model.User{}).Where("username = ?", "admin").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one admin user, got %d", count)
	}
	if !second.Migrator().HasTable(&model.Report{}) {
		t.Fatal("expected reports table to be auto-migrated")
	}
}

func TestOpenSQLiteUsesProvidedAdminPasswordWithoutForcedChange(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "postdare.db")
	database, err := Open(config.DatabaseConfig{Driver: "sqlite", Path: dbPath}, WithAdminPassword("admin-from-env"))
	if err != nil {
		t.Fatal(err)
	}
	var admin model.User
	if err := database.Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatal(err)
	}
	if admin.MustChangePassword {
		t.Fatal("expected provided admin password to skip forced password change")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte("admin-from-env")); err != nil {
		t.Fatal("provided password does not match stored hash")
	}
}
