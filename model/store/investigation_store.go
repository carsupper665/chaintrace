package store

import "time"

type InvestigationNetwork string

const (
	NetworkTRONMainnet InvestigationNetwork = "TRON_MAINNET"
)

type InvestigationStatus string

const (
	InvestigationPending   InvestigationStatus = "待處理"
	InvestigationAnalyzing InvestigationStatus = "分析中"
	InvestigationCompleted InvestigationStatus = "已完成"
)

type Investigation struct {
	ID                    string               `gorm:"type:varchar(24);primaryKey"`
	OwnerID               uint                 `gorm:"not null;index:idx_investigations_owner_created,priority:1"`
	Owner                 User                 `gorm:"foreignKey:OwnerID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
	Title                 string               `gorm:"size:120;not null"`
	Address               *string              `gorm:"size:64"`
	Network               InvestigationNetwork `gorm:"type:varchar(32);not null"`
	Status                InvestigationStatus  `gorm:"type:varchar(16);not null"`
	TargetLocked          bool                 `gorm:"not null;default:false"`
	ActiveAnalysisRunID   *string              `gorm:"type:varchar(24);index"`
	CurrentResultID       *string              `gorm:"type:varchar(24)"`
	RiskScore             *int
	RelatedNodes          int     `gorm:"not null;default:0"`
	TotalFlowSmallestUnit *string `gorm:"type:text"`
	TotalFlowDecimals     *int
	FlowAsset             *string   `gorm:"size:32"`
	TransactionCount      int       `gorm:"not null;default:0"`
	CreatedAt             time.Time `gorm:"index:idx_investigations_owner_created,priority:2"`
	UpdatedAt             time.Time
}
