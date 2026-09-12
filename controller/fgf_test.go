package controller

import (
	"chaintrace/model"
	"chaintrace/model/store"
	"testing"

	fgfoidc "github.com/carsupper665/Frog-Grid-Forge/fgf-oidc"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestFGFUserLinksByEmailAndFollowsIdPRole(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	previous := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previous; sqlDB, _ := db.DB(); sqlDB.Close() })
	if err := db.AutoMigrate(&store.User{}); err != nil {
		t.Fatal(err)
	}
	local := store.User{Username: "owner", DisplayName: "Local", Email: "owner@example.test", Role: 1, Password: "hash", Salt: "salt"}
	if err := db.Create(&local).Error; err != nil {
		t.Fatal(err)
	}
	key := fgfoidc.Config{Issuer: "https://idp.example.test"}.IdentityKey("1")

	// An existing local account with the IdP email is linked and takes the IdP role.
	user, err := fgfUser(key, fgfoidc.Identity{Subject: "1", Email: "owner@example.test", Name: "Root User", Role: 6})
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != local.ID || user.Username != "owner" || user.Role != 6 || user.DisplayName != "Root User" || user.FGFSubject == nil || *user.FGFSubject != key {
		t.Fatalf("existing account not linked: %+v", user)
	}

	// Later logins find the link by subject and sync role changes made in the IdP.
	user, err = fgfUser(key, fgfoidc.Identity{Subject: "1", Email: "owner@example.test", Name: "Root User", Role: 1})
	if err != nil || user.ID != local.ID || user.Role != 1 {
		t.Fatalf("role not synced: %+v, %v", user, err)
	}
	var stored store.User
	if err := db.First(&stored, local.ID).Error; err != nil || stored.Role != 1 || stored.FGFSubject == nil || *stored.FGFSubject != key {
		t.Fatalf("link not persisted: %+v, %v", stored, err)
	}

	// An unknown email creates a new account at the IdP role.
	other := fgfoidc.Config{Issuer: "https://idp.example.test"}.IdentityKey("2")
	user, err = fgfUser(other, fgfoidc.Identity{Subject: "2", Email: "new@example.test", Name: "New", Role: 4})
	if err != nil || user.ID == local.ID || user.Role != 4 || user.Username != "f_"+other[:10] || user.Password != "!" {
		t.Fatalf("new account not created: %+v, %v", user, err)
	}
	var count int64
	db.Model(&store.User{}).Count(&count)
	if count != 2 {
		t.Fatalf("expected 2 users, got %d", count)
	}
}
