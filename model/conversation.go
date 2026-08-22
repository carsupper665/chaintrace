package model

import (
	"chaintrace/model/store"
	"chaintrace/utils"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const ConversationChunkMaxBytes = 4096

const unavailableOutcomeContent = `{"code":"agent_unavailable"}`

var (
	ErrInvalidConversationCursor = errors.New("invalid conversation cursor")
	conversationWriteMu          sync.Mutex
)

type ConversationMessage struct {
	ID        string
	Role      store.ConversationRole
	Content   string
	CreatedAt time.Time
}

type ConversationPage struct {
	Messages   []ConversationMessage
	NextCursor *string
}

type conversationCursor struct {
	Version         int    `json:"v"`
	InvestigationID string `json:"i"`
	Ordinal         int64  `json:"o"`
	MessageID       string `json:"m"`
}

func PersistUnavailableConversation(ownerID uint, investigationID, idempotencyKey, content string) ([]ConversationMessage, error) {
	conversationWriteMu.Lock()
	defer conversationWriteMu.Unlock()

	var result []ConversationMessage
	err := DB.Transaction(func(tx *gorm.DB) error {
		var investigation store.Investigation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("owner_id = ? AND id = ?", ownerID, investigationID).
			First(&investigation).Error; err != nil {
			return err
		}

		var existingRecords []store.ConversationMessage
		if err := tx.Where("investigation_id = ? AND idempotency_key = ?", investigationID, idempotencyKey).
			Order("ordinal ASC").Find(&existingRecords).Error; err != nil {
			return err
		}
		existing, err := assembleConversationMessages(tx, existingRecords)
		if err != nil {
			return err
		}
		if len(existing) != 0 {
			if len(existing) != 2 || existing[0].Role != store.ConversationRoleUser || existing[1].Role != store.ConversationRoleSystem {
				return fmt.Errorf("incomplete idempotent conversation group")
			}
			result = existing
			return nil
		}

		var maxOrdinal sql.NullInt64
		if err := tx.Model(&store.ConversationMessage{}).
			Where("investigation_id = ?", investigationID).
			Select("MAX(ordinal)").Scan(&maxOrdinal).Error; err != nil {
			return err
		}
		firstOrdinal := int64(0)
		if maxOrdinal.Valid {
			firstOrdinal = maxOrdinal.Int64 + 1
		}
		createdAt := time.Now().UTC()
		records := []store.ConversationMessage{
			{InvestigationID: investigationID, Role: store.ConversationRoleUser, IdempotencyKey: idempotencyKey, Ordinal: firstOrdinal, CreatedAt: createdAt},
			{InvestigationID: investigationID, Role: store.ConversationRoleSystem, IdempotencyKey: idempotencyKey, Ordinal: firstOrdinal + 1, CreatedAt: createdAt},
		}
		contents := []string{content, unavailableOutcomeContent}
		result = make([]ConversationMessage, 0, len(records))
		for i := range records {
			id, err := utils.SecureRandomString(24)
			if err != nil {
				return err
			}
			records[i].ID = id
			if err := tx.Create(&records[i]).Error; err != nil {
				return err
			}
			for chunkIndex, chunk := range splitConversationContent(contents[i]) {
				record := store.ConversationChunk{MessageID: id, ChunkIndex: chunkIndex, Content: chunk}
				if err := tx.Create(&record).Error; err != nil {
					return err
				}
			}
			result = append(result, ConversationMessage{ID: id, Role: records[i].Role, Content: contents[i], CreatedAt: createdAt})
		}
		return nil
	})
	return result, err
}

func LoadConversationPage(ownerID uint, investigationID, encodedCursor string, pageSize int) (ConversationPage, error) {
	if err := DB.Where("owner_id = ? AND id = ?", ownerID, investigationID).First(&store.Investigation{}).Error; err != nil {
		return ConversationPage{}, err
	}
	database := DB.Where("investigation_id = ?", investigationID)
	if encodedCursor != "" {
		cursor, err := decodeConversationCursor(encodedCursor, investigationID)
		if err != nil {
			return ConversationPage{}, err
		}
		database = database.Where("ordinal > ?", cursor.Ordinal)
	}

	var records []store.ConversationMessage
	if err := database.Order("ordinal ASC").Limit(pageSize + 1).Find(&records).Error; err != nil {
		return ConversationPage{}, err
	}
	hasMore := len(records) > pageSize
	if hasMore {
		records = records[:pageSize]
	}
	messages, err := assembleConversationMessages(DB, records)
	if err != nil {
		return ConversationPage{}, err
	}
	page := ConversationPage{Messages: messages}
	if hasMore {
		last := records[len(records)-1]
		cursor, err := encodeConversationCursor(conversationCursor{
			Version: 1, InvestigationID: investigationID, Ordinal: last.Ordinal, MessageID: last.ID,
		})
		if err != nil {
			return ConversationPage{}, err
		}
		page.NextCursor = &cursor
	}
	return page, nil
}

func assembleConversationMessages(database *gorm.DB, records []store.ConversationMessage) ([]ConversationMessage, error) {
	messages := make([]ConversationMessage, 0, len(records))
	if len(records) == 0 {
		return messages, nil
	}
	messageIDs := make([]string, 0, len(records))
	for _, record := range records {
		messageIDs = append(messageIDs, record.ID)
	}
	var chunks []store.ConversationChunk
	if err := database.Where("message_id IN ?", messageIDs).
		Order("message_id ASC").Order("chunk_index ASC").Find(&chunks).Error; err != nil {
		return nil, err
	}
	contentByMessage := make(map[string]*strings.Builder, len(records))
	for _, chunk := range chunks {
		builder := contentByMessage[chunk.MessageID]
		if builder == nil {
			builder = &strings.Builder{}
			contentByMessage[chunk.MessageID] = builder
		}
		builder.WriteString(chunk.Content)
	}
	for _, record := range records {
		builder := contentByMessage[record.ID]
		if builder == nil {
			return nil, fmt.Errorf("conversation message %s has no chunks", record.ID)
		}
		messages = append(messages, ConversationMessage{
			ID: record.ID, Role: record.Role, Content: builder.String(), CreatedAt: record.CreatedAt,
		})
	}
	return messages, nil
}

func splitConversationContent(content string) []string {
	chunks := make([]string, 0, len(content)/ConversationChunkMaxBytes+1)
	for len(content) > 0 {
		end := min(len(content), ConversationChunkMaxBytes)
		if end < len(content) {
			for end > 0 && !utf8.RuneStart(content[end]) {
				end--
			}
		}
		chunks = append(chunks, content[:end])
		content = content[end:]
	}
	return chunks
}

func encodeConversationCursor(cursor conversationCursor) (string, error) {
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeConversationCursor(encoded, investigationID string) (conversationCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return conversationCursor{}, ErrInvalidConversationCursor
	}
	var cursor conversationCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.Version != 1 || cursor.InvestigationID != investigationID || cursor.Ordinal < 0 || cursor.MessageID == "" {
		return conversationCursor{}, ErrInvalidConversationCursor
	}
	return cursor, nil
}
