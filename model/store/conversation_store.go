package store

import "time"

type ConversationRole string

const (
	ConversationRoleUser   ConversationRole = "user"
	ConversationRoleSystem ConversationRole = "system"
	ConversationRoleAgent  ConversationRole = "agent"
)

type ConversationMessage struct {
	ID              string           `gorm:"type:varchar(24);primaryKey"`
	InvestigationID string           `gorm:"type:varchar(24);not null;uniqueIndex:idx_conversation_order,priority:1;uniqueIndex:idx_conversation_idempotency_role,priority:1;index"`
	Investigation   Investigation    `gorm:"foreignKey:InvestigationID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
	Role            ConversationRole `gorm:"type:varchar(16);not null;uniqueIndex:idx_conversation_idempotency_role,priority:3;check:conversation_message_role,role IN ('user','system','agent')"`
	IdempotencyKey  string           `gorm:"type:varchar(128);not null;uniqueIndex:idx_conversation_idempotency_role,priority:2"`
	Ordinal         int64            `gorm:"not null;uniqueIndex:idx_conversation_order,priority:2"`
	CreatedAt       time.Time        `gorm:"not null;index:idx_conversation_created"`
}

type ConversationChunk struct {
	MessageID  string              `gorm:"type:varchar(24);primaryKey"`
	Message    ConversationMessage `gorm:"foreignKey:MessageID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
	ChunkIndex int                 `gorm:"primaryKey;autoIncrement:false"`
	Content    string              `gorm:"type:text;not null"`
}
