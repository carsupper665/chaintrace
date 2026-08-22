package analysis

import (
	"chaintrace/model"
	"chaintrace/model/store"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrStaleDataset       = errors.New("dataset is not current")
	ErrInvalidGraphCursor = errors.New("invalid graph cursor")
)

type graphCursor struct {
	Version         int       `json:"v"`
	DatasetID       string    `json:"d"`
	Anchor          string    `json:"a,omitempty"`
	Timestamp       time.Time `json:"t"`
	TransactionHash string    `json:"h"`
	EventIdentity   string    `json:"e"`
}

type GraphNode struct {
	ID      string `json:"id"`
	Address string `json:"address"`
	Type    string `json:"type"`
}

type GraphEdge struct {
	ID              string      `json:"id"`
	TransactionHash string      `json:"transactionHash"`
	EventIdentity   string      `json:"eventIdentity"`
	From            string      `json:"from"`
	To              string      `json:"to"`
	Amount          ExactAmount `json:"amount"`
	Timestamp       time.Time   `json:"timestamp"`
}

type GraphPage struct {
	DatasetID  string      `json:"datasetId"`
	Nodes      []GraphNode `json:"nodes"`
	Edges      []GraphEdge `json:"edges"`
	NextCursor *string     `json:"nextCursor"`
	HasMore    bool        `json:"hasMore"`
}

func LoadGraphPage(ownerID uint, investigationID, datasetID, encodedCursor, anchor string, pageSize int) (GraphPage, error) {
	var investigation store.Investigation
	if err := model.DB.Where("owner_id = ? AND id = ?", ownerID, investigationID).First(&investigation).Error; err != nil {
		return GraphPage{}, err
	}
	if investigation.CurrentResultID == nil || *investigation.CurrentResultID != datasetID {
		return GraphPage{}, ErrStaleDataset
	}

	database := model.DB.Where("dataset_id = ?", datasetID)
	if anchor != "" {
		database = database.Where("from_address = ? OR to_address = ?", anchor, anchor)
	}
	if encodedCursor != "" {
		cursor, err := decodeGraphCursor(encodedCursor, datasetID, anchor)
		if err != nil {
			return GraphPage{}, err
		}
		database = database.Where(
			"timestamp > ? OR (timestamp = ? AND transaction_hash > ?) OR (timestamp = ? AND transaction_hash = ? AND event_identity > ?)",
			cursor.Timestamp,
			cursor.Timestamp, cursor.TransactionHash,
			cursor.Timestamp, cursor.TransactionHash, cursor.EventIdentity,
		)
	}
	var transfers []store.TRC20Transfer
	if err := database.
		Order("timestamp ASC").
		Order("transaction_hash ASC").
		Order("event_identity ASC").
		Limit(pageSize + 1).
		Find(&transfers).Error; err != nil {
		return GraphPage{}, err
	}

	page := GraphPage{
		DatasetID: datasetID,
		Nodes:     make([]GraphNode, 0),
	}
	if len(transfers) > pageSize {
		transfers = transfers[:pageSize]
		last := transfers[len(transfers)-1]
		nextCursor, err := encodeGraphCursor(graphCursor{
			Version:         1,
			DatasetID:       datasetID,
			Anchor:          anchor,
			Timestamp:       last.Timestamp,
			TransactionHash: last.TransactionHash,
			EventIdentity:   last.EventIdentity,
		})
		if err != nil {
			return GraphPage{}, err
		}
		page.NextCursor = &nextCursor
		page.HasMore = true
	}
	page.Edges = make([]GraphEdge, 0, len(transfers))
	nodes := make(map[string]struct{})
	for _, transfer := range transfers {
		page.Edges = append(page.Edges, GraphEdge{
			ID:              transfer.TransactionHash + ":" + transfer.EventIdentity,
			TransactionHash: transfer.TransactionHash,
			EventIdentity:   transfer.EventIdentity,
			From:            transfer.FromAddress,
			To:              transfer.ToAddress,
			Amount: ExactAmount{
				SmallestUnit: transfer.AmountSmallestUnit,
				Decimals:     transfer.Decimals,
				Asset:        transfer.Asset,
			},
			Timestamp: transfer.Timestamp,
		})
		for _, address := range []string{transfer.FromAddress, transfer.ToAddress} {
			if _, exists := nodes[address]; exists {
				continue
			}
			nodeType := "normal"
			if investigation.Address != nil && address == *investigation.Address {
				nodeType = "focus"
			}
			page.Nodes = append(page.Nodes, GraphNode{ID: address, Address: address, Type: nodeType})
			nodes[address] = struct{}{}
		}
	}
	return page, nil
}

func encodeGraphCursor(cursor graphCursor) (string, error) {
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeGraphCursor(encoded, datasetID, anchor string) (graphCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return graphCursor{}, ErrInvalidGraphCursor
	}
	var cursor graphCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.Version != 1 || cursor.DatasetID != datasetID || cursor.Anchor != anchor || cursor.Timestamp.IsZero() || cursor.TransactionHash == "" || cursor.EventIdentity == "" {
		return graphCursor{}, ErrInvalidGraphCursor
	}
	return cursor, nil
}
