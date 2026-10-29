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
	// Counterparties is how many distinct addresses this node transfers with
	// in the whole Dataset, so a client can tell whether expanding it can
	// reveal anything beyond what it already shows.
	Counterparties int `json:"counterparties"`
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

// top > 0 returns the anchor's transfers with its counterparties ranked
// skip+1..skip+top by transfer count; every such transfer is returned so no
// ranked counterparty is cut off by a page boundary.
func LoadGraphPage(ownerID uint, investigationID, datasetID, encodedCursor, anchor string, pageSize, start, end, top, skip int) (GraphPage, error) {
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
	if top > 0 {
		var counterparties []string
		if err := model.DB.Model(&store.TRC20Transfer{}).
			Where("dataset_id = ? AND (from_address = ? OR to_address = ?)", datasetID, anchor, anchor).
			Select("CASE WHEN from_address = ? THEN to_address ELSE from_address END AS counterparty", anchor).
			Group("counterparty").
			Order("COUNT(*) DESC").
			Order("counterparty ASC").
			Offset(skip).
			Limit(top).
			Pluck("counterparty", &counterparties).Error; err != nil {
			return GraphPage{}, err
		}
		if len(counterparties) == 0 {
			page := buildGraphPage(nil, investigation)
			page.DatasetID = datasetID
			return page, nil
		}
		database = database.Where(
			"(from_address = ? AND to_address IN ?) OR (to_address = ? AND from_address IN ?)",
			anchor, counterparties, anchor, counterparties,
		)
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
	query := database.
		Order("timestamp ASC").
		Order("transaction_hash ASC").
		Order("event_identity ASC")
	limit := pageSize + 1
	if start > 0 {
		query = query.Offset(start - 1)
		limit = end - start + 2
	} else if top > 0 {
		limit = -1
	}
	var transfers []store.TRC20Transfer
	if err := query.Limit(limit).Find(&transfers).Error; err != nil {
		return GraphPage{}, err
	}

	requested := pageSize
	if start > 0 {
		requested = end - start + 1
	}
	hasMore := top == 0 && len(transfers) > requested
	if hasMore {
		transfers = transfers[:requested]
	}
	page := buildGraphPage(transfers, investigation)
	page.DatasetID = datasetID
	if err := countCounterparties(datasetID, page.Nodes); err != nil {
		return GraphPage{}, err
	}
	if hasMore && start == 0 && top == 0 {
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
	}
	page.HasMore = hasMore
	return page, nil
}

func buildGraphPage(transfers []store.TRC20Transfer, investigation store.Investigation) GraphPage {
	page := GraphPage{Nodes: make([]GraphNode, 0)}
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
	return page
}

func countCounterparties(datasetID string, nodes []GraphNode) error {
	if len(nodes) == 0 {
		return nil
	}
	addresses := make([]string, 0, len(nodes))
	for _, node := range nodes {
		addresses = append(addresses, node.Address)
	}
	var rows []struct {
		Address        string
		Counterparties int
	}
	if err := model.DB.Raw(
		`SELECT address, COUNT(DISTINCT other) AS counterparties FROM (
			SELECT from_address AS address, to_address AS other FROM trc20_transfers WHERE dataset_id = ? AND from_address IN ?
			UNION ALL
			SELECT to_address AS address, from_address AS other FROM trc20_transfers WHERE dataset_id = ? AND to_address IN ?
		) AS sides GROUP BY address`,
		datasetID, addresses, datasetID, addresses,
	).Scan(&rows).Error; err != nil {
		return err
	}
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		counts[row.Address] = row.Counterparties
	}
	for index := range nodes {
		nodes[index].Counterparties = counts[nodes[index].Address]
	}
	return nil
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
