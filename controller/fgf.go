package controller

import (
	"chaintrace/auth"
	"chaintrace/model"
	"chaintrace/model/store"
	"chaintrace/utils"
	"errors"
	fgfoidc "github.com/carsupper665/Frog-Grid-Forge/fgf-oidc"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"net/http"
	"os"
)

func fgfConfig() fgfoidc.Config { return fgfoidc.FromEnv(os.Getenv, "chaintrace_fgf_flow") }
func FGFLogin(c *gin.Context) {
	if err := fgfConfig().Begin(c.Writer, c.Request); err != nil {
		utils.SysLog.Errorf("FGF login begin: %v, Request ID: %s", err, c.Request.Context().Value(utils.RequestIdKey))
		c.Redirect(http.StatusFound, "/login?error=fgf_unavailable")
	}
}
func FGFCallback(c *gin.Context) {
	var input struct {
		Code  string `json:"code"`
		State string `json:"state"`
	}
	if c.ShouldBindJSON(&input) != nil {
		c.JSON(400, gin.H{"error": "invalid_callback"})
		return
	}
	cfg := fgfConfig()
	identity, err := cfg.Exchange(c.Writer, c.Request, input.Code, input.State)
	if err != nil {
		c.JSON(401, gin.H{"error": "invalid_callback"})
		return
	}
	user, err := fgfUser(cfg.IdentityKey(identity.Subject), identity)
	if err != nil {
		utils.SysLog.Errorf("FGF account for %s: %v, Request ID: %s", identity.Email, err, c.Request.Context().Value(utils.RequestIdKey))
		c.JSON(403, gin.H{"error": "fgf_account_unavailable"})
		return
	}
	token, err := auth.GenJWT(user.ID, c.ClientIP())
	if err != nil {
		c.JSON(500, gin.H{"error": "session_failed"})
		return
	}
	c.JSON(200, gin.H{"token": token})
}

// fgfUser resolves the local account for an FGF identity: the account already
// linked to it, else the account with the same email (linked now), else a new
// one. Display name and role always follow the IdP.
func fgfUser(key string, identity fgfoidc.Identity) (store.User, error) {
	var user store.User
	err := model.DB.Where("fgf_subject = ?", key).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = model.DB.Where("email = ?", identity.Email).First(&user).Error
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		user = store.User{Username: "f_" + key[:10], DisplayName: identity.Name, Email: identity.Email, Role: identity.Role, Password: "!", Salt: "oidc", FGFSubject: &key}
		if err = model.DB.Create(&user).Error; err != nil {
			// A concurrent callback may have created this exact identity already.
			err = model.DB.Where("fgf_subject = ?", key).First(&user).Error
		}
		return user, err
	}
	if err != nil {
		return user, err
	}
	updates := map[string]any{}
	if user.FGFSubject == nil || *user.FGFSubject != key {
		updates["fgf_subject"] = key
	}
	if user.DisplayName != identity.Name {
		updates["display_name"] = identity.Name
	}
	if user.Role != identity.Role {
		updates["role"] = identity.Role
	}
	if len(updates) == 0 {
		return user, nil
	}
	if err := model.DB.Model(&user).Updates(updates).Error; err != nil {
		return user, err
	}
	user.FGFSubject, user.DisplayName, user.Role = &key, identity.Name, identity.Role
	return user, nil
}
