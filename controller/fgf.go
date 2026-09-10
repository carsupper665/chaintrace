package controller

import (
	"chaintrace/auth"
	"chaintrace/model"
	"chaintrace/model/store"
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
	key := cfg.IdentityKey(identity.Subject)
	var user store.User
	err = model.DB.Where("fgf_subject = ?", key).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// Never attach an external identity to an existing account by email alone.
		// New FGF accounts start at the service's ordinary-user privilege level.
		user = store.User{Username: "f_" + key[:10], DisplayName: identity.Name, Email: identity.Email, Role: 1, Password: "!", Salt: "oidc", FGFSubject: &key}
		err = model.DB.Create(&user).Error
		if err != nil {
			// A concurrent callback may have created this exact identity already.
			err = model.DB.Where("fgf_subject = ?", key).First(&user).Error
		}
	}
	if err != nil {
		c.JSON(403, gin.H{"error": "fgf_account_unavailable"})
		return
	}
	if user.DisplayName != identity.Name {
		if err := model.DB.Model(&user).Update("display_name", identity.Name).Error; err != nil {
			c.JSON(500, gin.H{"error": "profile_update_failed"})
			return
		}
		user.DisplayName = identity.Name
	}
	token, err := auth.GenJWT(user.ID, c.ClientIP())
	if err != nil {
		c.JSON(500, gin.H{"error": "session_failed"})
		return
	}
	c.JSON(200, gin.H{"token": token})
}
