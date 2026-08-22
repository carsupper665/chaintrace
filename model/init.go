package model

import (
	"chaintrace/auth"
	"chaintrace/model/store"
	"chaintrace/utils"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormLogger "gorm.io/gorm/logger"
)

var DB *gorm.DB
var logger *utils.SysLogger

func InitDb() error {
	logger = utils.SysLog

	db, err := Factory()
	if err != nil {
		logger.Errorf("Failed to connect to database: %v", err)
		return err
	}
	DB = db
	if err := migrateDB(); err != nil {
		logger.Errorf("Failed to migrate database: %v", err)
		return err
	}
	if err := reconcileLostAnalysisRuns(); err != nil {
		logger.Errorf("Failed to reconcile lost analysis runs: %v", err)
	}

	if utils.RootUser == "" || utils.RootUserEmail == "" || utils.RootPassword == "" {
		logger.Info("Root user data not set; skipping root user creation")
	} else if RootUserExists() {
		logger.Info("Root User Exists, skip create root user")
	} else {
		if err := createRoot(); err != nil {
			logger.Errorf("Failed to create root user: %v", err)
		}
	}

	logger.Info("Database migrated")
	return nil
}

// reconcileLostAnalysisRuns clears Investigations that a previous process left at
// InvestigationAnalyzing. Analysis run state is held in process memory, so none of
// those runs survived the restart.
func reconcileLostAnalysisRuns() error {
	result := DB.Model(&store.Investigation{}).
		Where("status = ?", store.InvestigationAnalyzing).
		Updates(map[string]any{
			"status": gorm.Expr(
				"CASE WHEN current_result_id IS NULL THEN ? ELSE ? END",
				store.InvestigationPending,
				store.InvestigationCompleted,
			),
			"active_analysis_run_id": nil,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		logger.Infof("Reset %d investigation(s) left analyzing by a previous process", result.RowsAffected)
	}
	return nil
}

func Factory() (*gorm.DB, error) {
	dsn := utils.PostgreDSN
	if dsn == "" {
		if !utils.IsLocalMode() {
			return nil, fmt.Errorf("POSTGRES_DSN is required outside local/test mode")
		}
		return initSqliteDB()
	}
	return initPostgreSQLDB(dsn, true)
}
func initSqliteDB() (*gorm.DB, error) {
	separator := "?"
	if strings.Contains(utils.SQLitePath, "?") {
		separator = "&"
	}
	return gorm.Open(sqlite.Open(utils.SQLitePath+separator+"_pragma=foreign_keys(1)"), &gorm.Config{
		PrepareStmt: true, // precompile SQL
	})
}

func initPostgreSQLDB(dsn string, isLog bool) (*gorm.DB, error) {
	cfg := &gorm.Config{}
	if isLog {
		cfg.Logger = gormLogger.New(log.New(os.Stdout, "\r\n", log.LstdFlags), gormLogger.Config{
			SlowThreshold:             time.Second,
			LogLevel:                  gormLogger.Warn,
			IgnoreRecordNotFoundError: true,
			ParameterizedQueries:      true,
			Colorful:                  false,
		})
	}

	db, err := gorm.Open(postgres.Open(dsn), cfg)
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err == nil {
		sqlDB.SetMaxIdleConns(10)
		sqlDB.SetMaxOpenConns(100)
		sqlDB.SetConnMaxLifetime(time.Hour)
	}

	return db, nil
}

func migrateDB() error {
	err := DB.AutoMigrate(
		&store.User{},
		&store.Investigation{},
		&store.AnalysisDataset{},
		&store.BlockchainTransaction{},
		&store.TRC20Transfer{},
		&store.InvestigationMetrics{},
		&store.Assessment{},
		&store.ConversationMessage{},
		&store.ConversationChunk{},
	)
	return err
}
func createRoot() error {
	username := utils.RootUser
	email := utils.RootUserEmail
	password := utils.RootPassword
	salt, err := utils.SecureRandomString(16)
	if err != nil {
		return err
	}

	sp := password + salt
	hashPassword, err := auth.P2H(sp)
	if err != nil {
		return err
	}

	// create user
	rootUser := store.User{
		Username:    username,
		DisplayName: "Root User",
		Role:        utils.RoleRootUser,
		Email:       email,
		Password:    hashPassword,
		Salt:        salt,
	}

	err = DB.Create(&rootUser).Error
	if err != nil {
		return err
	}
	return nil
}

func RootUserExists() bool {
	var user store.User
	err := DB.Where("role = ?", utils.RoleRootUser).First(&user).Error
	return err == nil
}
