package store

import "time"

type AnalysisDataset struct {
	ID                 string               `gorm:"type:varchar(24);primaryKey"`
	InvestigationID    string               `gorm:"type:varchar(24);not null;index"`
	Investigation      Investigation        `gorm:"foreignKey:InvestigationID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
	AnalysisRunID      string               `gorm:"type:varchar(24);not null;uniqueIndex"`
	Network            InvestigationNetwork `gorm:"type:varchar(32);not null"`
	Asset              string               `gorm:"type:varchar(32);not null"`
	CutoffBlockID      string               `gorm:"size:128;not null"`
	WindowStart        time.Time            `gorm:"not null"`
	WindowEnd          time.Time            `gorm:"not null"`
	TransferLimit      int                  `gorm:"not null"`
	TraversalDepth     int                  `gorm:"not null"`
	CollectedTransfers int                  `gorm:"not null"`
	ReachedDepth       int                  `gorm:"not null"`
	Partial            bool                 `gorm:"not null"`
	Confidence         int                  `gorm:"not null"`
	StopReason         string               `gorm:"size:64;not null"`
	CreatedAt          time.Time            `gorm:"not null"`
}

type BlockchainTransaction struct {
	ID              string               `gorm:"type:varchar(24);primaryKey"`
	DatasetID       string               `gorm:"type:varchar(24);not null;uniqueIndex:idx_dataset_transaction,priority:1;index"`
	Dataset         AnalysisDataset      `gorm:"foreignKey:DatasetID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
	Network         InvestigationNetwork `gorm:"type:varchar(32);not null;uniqueIndex:idx_dataset_transaction,priority:2"`
	TransactionHash string               `gorm:"size:128;not null;uniqueIndex:idx_dataset_transaction,priority:3"`
	BlockID         string               `gorm:"size:128;not null"`
	BlockTimestamp  time.Time            `gorm:"not null"`
	Successful      bool                 `gorm:"not null"`
	Confirmed       bool                 `gorm:"not null"`
	CreatedAt       time.Time            `gorm:"not null"`
}

type TRC20Transfer struct {
	ID                      string                `gorm:"type:varchar(24);primaryKey"`
	DatasetID               string                `gorm:"type:varchar(24);not null;uniqueIndex:idx_dataset_transfer_event,priority:1;index"`
	Dataset                 AnalysisDataset       `gorm:"foreignKey:DatasetID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
	BlockchainTransactionID string                `gorm:"type:varchar(24);not null;index"`
	BlockchainTransaction   BlockchainTransaction `gorm:"foreignKey:BlockchainTransactionID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
	TransactionHash         string                `gorm:"size:128;not null;uniqueIndex:idx_dataset_transfer_event,priority:2"`
	EventIdentity           string                `gorm:"size:128;not null;uniqueIndex:idx_dataset_transfer_event,priority:3"`
	ContractAddress         string                `gorm:"size:64;not null"`
	Asset                   string                `gorm:"size:32;not null"`
	FromAddress             string                `gorm:"size:64;not null"`
	ToAddress               string                `gorm:"size:64;not null"`
	AmountSmallestUnit      string                `gorm:"type:text;not null"`
	Decimals                int                   `gorm:"not null"`
	Timestamp               time.Time             `gorm:"not null"`
	CreatedAt               time.Time             `gorm:"not null"`
}

type InvestigationMetrics struct {
	DatasetID             string          `gorm:"type:varchar(24);primaryKey"`
	Dataset               AnalysisDataset `gorm:"foreignKey:DatasetID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
	RelatedNodes          int             `gorm:"not null"`
	TransferCount         int             `gorm:"not null"`
	TotalFlowSmallestUnit string          `gorm:"type:text;not null"`
	TotalFlowDecimals     int             `gorm:"not null"`
	Asset                 string          `gorm:"size:32;not null"`
	CreatedAt             time.Time       `gorm:"not null"`
}

type Assessment struct {
	DatasetID           string          `gorm:"type:varchar(24);primaryKey"`
	Dataset             AnalysisDataset `gorm:"foreignKey:DatasetID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
	Score               *int
	Level               string `gorm:"size:32"`
	ReasonsJSON         string `gorm:"type:text;not null"`
	NodeAssessmentsJSON string `gorm:"type:text;not null"`
	Source              string `gorm:"size:64;not null"`
	// LearnedScore is the unsupervised model's anomaly score (ADR-0015), stored
	// alongside Score rather than in place of it. It is an unbounded signed
	// float, not a 0-100 rules-style score, and nil whenever no scorer was
	// configured or the attempt failed — that is not an error for the run.
	LearnedScore *float64
	// LearnedScoreSource echoes the model manifest's trainingDataHash so a
	// stored score can be traced to the artifact that produced it. "" when
	// LearnedScore is nil.
	LearnedScoreSource string    `gorm:"size:64"`
	UpdatedAt          time.Time `gorm:"not null"`
}
