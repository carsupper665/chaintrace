package model

import (
	"chaintrace/utils"
	"testing"
)

func TestFactoryDoesNotFallbackToSQLiteInProduction(t *testing.T) {
	previousEnvironment := utils.Environment
	previousDebug := utils.DebugMode
	previousDSN := utils.PostgreDSN
	previousSQLitePath := utils.SQLitePath
	t.Cleanup(func() {
		utils.Environment = previousEnvironment
		utils.DebugMode = previousDebug
		utils.PostgreDSN = previousDSN
		utils.SQLitePath = previousSQLitePath
	})
	utils.Environment = "production"
	utils.DebugMode = true
	utils.PostgreDSN = ""
	utils.SQLitePath = "file:production_fallback_test?mode=memory&cache=shared"

	if _, err := Factory(); err == nil {
		t.Fatal("Factory() without production PostgreSQL DSN fell back to SQLite")
	}
}
