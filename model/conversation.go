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

// conversationEntry is one message to append within an idempotent group.
type conversationEntry struct {
	Role    store.ConversationRole
	Content string
}

// PersistUnavailableConversation records the Owner's message plus the machine
// outcome written when no Agent answered.
func PersistUnavailableConversation(ownerID uint, investigationID, idempotencyKey, content string) ([]ConversationMessage, error) {
	return persistConversationGroup(ownerID, investigationID, idempotencyKey, []conversationEntry{
		{Role: store.ConversationRoleUser, Content: content},
		{Role: store.ConversationRoleSystem, Content: unavailableOutcomeContent},
	})
}

// PersistConversationTurn records one completed exchange: what the Owner asked
// and what the Agent answered.
func PersistConversationTurn(ownerID uint, investigationID, idempotencyKey, question, answer string) ([]ConversationMessage, error) {
	return persistConversationGroup(ownerID, investigationID, idempotencyKey, []conversationEntry{
		{Role: store.ConversationRoleUser, Content: question},
		{Role: store.ConversationRoleAgent, Content: answer},
	})
}

// PersistAgentMessage records a single Agent message with no Owner question
// before it, which is how an Investigation summary enters the conversation.
func PersistAgentMessage(ownerID uint, investigationID, idempotencyKey, content string) ([]ConversationMessage, error) {
	return persistConversationGroup(ownerID, investigationID, idempotencyKey, []conversationEntry{
		{Role: store.ConversationRoleAgent, Content: content},
	})
}

// persistConversationGroup appends one idempotent group of messages.
//
// Replaying the same idempotency key returns the stored group untouched, which
// is what makes a retried request — or a second summary request for the same
// dataset — safe.
func persistConversationGroup(
	ownerID uint, investigationID, idempotencyKey string, entries []conversationEntry,
) ([]ConversationMessage, error) {
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
			if !matchesEntryRoles(existing, entries) {
				return fmt.Errorf("incomplete idempotent conversation group")
			}
			result = existing
			return nil
		}

		firstOrdinal, err := nextConversationOrdinal(tx, investigationID)
		if err != nil {
			return err
		}
		result, err = appendConversationEntries(tx, investigationID, idempotencyKey, entries, firstOrdinal)
		return err
	})
	return result, err
}

func nextConversationOrdinal(tx *gorm.DB, investigationID string) (int64, error) {
	var maxOrdinal sql.NullInt64
	if err := tx.Model(&store.ConversationMessage{}).
		Where("investigation_id = ?", investigationID).
		Select("MAX(ordinal)").Scan(&maxOrdinal).Error; err != nil {
		return 0, err
	}
	if !maxOrdinal.Valid {
		return 0, nil
	}
	return maxOrdinal.Int64 + 1, nil
}

// appendConversationEntries writes each entry as a message plus its content
// chunks. All entries share one timestamp so the group reads as one exchange.
func appendConversationEntries(
	tx *gorm.DB, investigationID, idempotencyKey string,
	entries []conversationEntry, firstOrdinal int64,
) ([]ConversationMessage, error) {
	createdAt := time.Now().UTC()
	messages := make([]ConversationMessage, 0, len(entries))
	for offset, entry := range entries {
		id, err := utils.SecureRandomString(24)
		if err != nil {
			return nil, err
		}
		record := store.ConversationMessage{
			ID:              id,
			InvestigationID: investigationID,
			Role:            entry.Role,
			IdempotencyKey:  idempotencyKey,
			Ordinal:         firstOrdinal + int64(offset),
			CreatedAt:       createdAt,
		}
		if err := tx.Create(&record).Error; err != nil {
			return nil, err
		}
		if err := writeConversationChunks(tx, id, entry.Content); err != nil {
			return nil, err
		}
		messages = append(messages, ConversationMessage{
			ID: id, Role: entry.Role, Content: entry.Content, CreatedAt: createdAt,
		})
	}
	return messages, nil
}

func writeConversationChunks(tx *gorm.DB, messageID, content string) error {
	for chunkIndex, chunk := range splitConversationContent(content) {
		record := store.ConversationChunk{MessageID: messageID, ChunkIndex: chunkIndex, Content: chunk}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
	}
	return nil
}

func matchesEntryRoles(existing []ConversationMessage, entries []conversationEntry) bool {
	if len(existing) != len(entries) {
		return false
	}
	for index := range entries {
		if existing[index].Role != entries[index].Role {
			return false
		}
	}
	return true
}

// FindConversationGroup returns the messages already stored under an
// idempotency key, or an empty slice when the key is unused.
//
// It lets a caller answer a repeated request from storage instead of doing the
// work again — which for an Agent turn means not spending tokens twice.
func FindConversationGroup(ownerID uint, investigationID, idempotencyKey string) ([]ConversationMessage, error) {
	if err := DB.Where("owner_id = ? AND id = ?", ownerID, investigationID).
		First(&store.Investigation{}).Error; err != nil {
		return nil, err
	}
	var records []store.ConversationMessage
	if err := DB.Where("investigation_id = ? AND idempotency_key = ?", investigationID, idempotencyKey).
		Order("ordinal ASC").Find(&records).Error; err != nil {
		return nil, err
	}
	return assembleConversationMessages(DB, records)
}

// LoadRecentConversation returns the newest messages in chronological order.
//
// LoadConversationPage pages forward from the oldest message, which is right
// for rendering a transcript but wrong for building an Agent's context: what
// matters there is the tail of the conversation.
func LoadRecentConversation(ownerID uint, investigationID string, limit int) ([]ConversationMessage, error) {
	if err := DB.Where("owner_id = ? AND id = ?", ownerID, investigationID).
		First(&store.Investigation{}).Error; err != nil {
		return nil, err
	}
	var records []store.ConversationMessage
	if err := DB.Where("investigation_id = ?", investigationID).
		Order("ordinal DESC").Limit(limit).Find(&records).Error; err != nil {
		return nil, err
	}
	for left, right := 0, len(records)-1; left < right; left, right = left+1, right-1 {
		records[left], records[right] = records[right], records[left]
	}
	return assembleConversationMessages(DB, records)
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
