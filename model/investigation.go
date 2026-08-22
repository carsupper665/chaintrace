package model

import (
	"chaintrace/model/store"
	"errors"
	"strings"

	"gorm.io/gorm"
)

var ErrInvestigationTargetImmutable = errors.New("investigation target is immutable")

func AddInvestigation(investigation *store.Investigation) error {
	return DB.Create(investigation).Error
}

func ListInvestigations(ownerID uint, query string) ([]store.Investigation, error) {
	investigations := make([]store.Investigation, 0)
	database := DB.Where("owner_id = ?", ownerID)
	if query = strings.TrimSpace(query); query != "" {
		pattern := "%" + strings.ToLower(query) + "%"
		database = database.Where("(LOWER(title) LIKE ? OR LOWER(address) LIKE ?)", pattern, pattern)
	}
	err := database.
		Order("created_at ASC").
		Order("id ASC").
		Find(&investigations).Error
	return investigations, err
}

func GetInvestigation(ownerID uint, id string) (*store.Investigation, error) {
	investigation := &store.Investigation{}
	err := DB.Where("owner_id = ? AND id = ?", ownerID, id).First(investigation).Error
	return investigation, err
}

func UpdateInvestigation(ownerID uint, id string, changes map[string]any, targetUpdate bool) (*store.Investigation, error) {
	if len(changes) > 0 {
		database := DB.Model(&store.Investigation{}).Where("owner_id = ? AND id = ?", ownerID, id)
		if targetUpdate {
			database = database.Where(
				"status = ? AND target_locked = ? AND (active_analysis_run_id IS NULL OR active_analysis_run_id = ?)",
				store.InvestigationPending, false, "",
			)
		}
		result := database.Updates(changes)
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			if _, err := GetInvestigation(ownerID, id); err != nil {
				return nil, err
			}
			if targetUpdate {
				return nil, ErrInvestigationTargetImmutable
			}
			return nil, gorm.ErrRecordNotFound
		}
	}
	return GetInvestigation(ownerID, id)
}

func DeleteInvestigation(ownerID uint, id string) error {
	result := DB.Where("owner_id = ? AND id = ?", ownerID, id).Delete(&store.Investigation{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
