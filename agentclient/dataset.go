package agentclient

import (
	"chaintrace/model"
	"chaintrace/model/store"
)

// datasetView is one Analysis Dataset loaded into memory: its transfers, the
// set of addresses that appear in it, and per-address aggregates.
//
// It is immutable, because a dataset is immutable — a new Analysis Run creates
// a new dataset id rather than changing this one. That is what makes caching it
// by dataset id safe with no invalidation path.
//
// Both the evidence pack and every Tier 1 tool read from this one view, so a
// turn loads the dataset at most once.
type datasetView struct {
	datasetID string
	transfers []store.TRC20Transfer
	addresses map[string]struct{}
	stats     map[string]*addressStat
}

// loadDatasetView reads the whole dataset. Datasets are bounded by
// TransferLimit (system maximum 5000), so loading it whole is cheap and keeps
// big-integer aggregation exact in Go instead of in SQL.
func loadDatasetView(datasetID string) (*datasetView, error) {
	var transfers []store.TRC20Transfer
	if err := model.DB.Where("dataset_id = ?", datasetID).
		Order("timestamp ASC").Order("transaction_hash ASC").Order("event_identity ASC").
		Find(&transfers).Error; err != nil {
		return nil, err
	}
	return newDatasetView(datasetID, transfers), nil
}

func newDatasetView(datasetID string, transfers []store.TRC20Transfer) *datasetView {
	addresses := make(map[string]struct{})
	for _, transfer := range transfers {
		addresses[transfer.FromAddress] = struct{}{}
		addresses[transfer.ToAddress] = struct{}{}
	}
	return &datasetView{
		datasetID: datasetID,
		transfers: transfers,
		addresses: addresses,
		stats:     aggregate(transfers),
	}
}

// inScope reports whether an address appears anywhere in this dataset. It is
// the check that makes a hallucinated address harmless: the worst outcome is a
// refusal, never a fetch of data outside the Analysis Scope.
func (v *datasetView) inScope(address string) bool {
	_, ok := v.addresses[address]
	return ok
}
