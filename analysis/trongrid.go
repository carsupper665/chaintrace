package analysis

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"chaintrace/utils"
)

const (
	tronGridAPIKeyHeader       = "TRON-PRO-API-KEY"
	tronTransferEventTopic     = "ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
	tronMainnetUSDTHexAddress  = "a614f803b6fd780986a42c78ec9c7f77e6ded13c"
	tronBase58Alphabet         = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	maximumTronGridCursorBytes = 4096
)

type TronGridConfig struct {
	APIKey           string
	BaseURL          string
	HTTPClient       *http.Client
	HTTPTimeout      time.Duration
	MaxRetries       int
	RetryBaseDelay   time.Duration
	MaxRetryDelay    time.Duration
	MaxResponseBytes int64
	PageLimit        int
	MaxPages         int
}

type TronGridProvider struct {
	apiKey           string
	baseURL          *url.URL
	httpClient       *http.Client
	httpTimeout      time.Duration
	maxRetries       int
	retryBaseDelay   time.Duration
	maxRetryDelay    time.Duration
	maxResponseBytes int64
	pageLimit        int
	maxPages         int
}

func NewTronGridProvider(config TronGridConfig) (*TronGridProvider, error) {
	baseURL, err := url.Parse(strings.TrimRight(config.BaseURL, "/"))
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" {
		return nil, errors.New("invalid TronGrid base URL")
	}
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, errors.New("TronGrid API key is required")
	}
	if config.HTTPTimeout <= 0 || config.MaxRetries < 0 || config.RetryBaseDelay <= 0 || config.MaxRetryDelay <= 0 ||
		config.MaxResponseBytes < 1 || config.PageLimit < 1 || config.PageLimit > 200 || config.MaxPages < 1 {
		return nil, errors.New("invalid TronGrid bounds")
	}
	client := config.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	return &TronGridProvider{
		apiKey:           strings.TrimSpace(config.APIKey),
		baseURL:          baseURL,
		httpClient:       client,
		httpTimeout:      config.HTTPTimeout,
		maxRetries:       config.MaxRetries,
		retryBaseDelay:   config.RetryBaseDelay,
		maxRetryDelay:    config.MaxRetryDelay,
		maxResponseBytes: config.MaxResponseBytes,
		pageLimit:        config.PageLimit,
		maxPages:         config.MaxPages,
	}, nil
}

func ProductionTronGridProvider() ChainDataProvider {
	if strings.TrimSpace(utils.TronGridAPIKey) == "" {
		return nil
	}
	provider, err := NewTronGridProvider(TronGridConfig{
		APIKey:           utils.TronGridAPIKey,
		BaseURL:          utils.TronGridBaseURL,
		HTTPTimeout:      utils.TronGridHTTPTimeout,
		MaxRetries:       utils.TronGridMaxRetries,
		RetryBaseDelay:   utils.TronGridRetryBaseDelay,
		MaxRetryDelay:    utils.TronGridMaxRetryDelay,
		MaxResponseBytes: utils.TronGridMaxResponseBytes,
		PageLimit:        utils.TronGridPageLimit,
		MaxPages:         utils.TronGridMaxPages,
	})
	if err != nil {
		return nil
	}
	return provider
}

func (p *TronGridProvider) CaptureConfirmedCutoff(ctx context.Context, network string) (BlockCutoff, error) {
	if network != "TRON_MAINNET" {
		return BlockCutoff{}, errors.New("TronGrid supports only TRON mainnet")
	}
	var response struct {
		BlockID string `json:"blockID"`
		Header  struct {
			RawData struct {
				Timestamp int64 `json:"timestamp"`
			} `json:"raw_data"`
		} `json:"block_header"`
	}
	if err := p.requestJSON(ctx, http.MethodPost, "/walletsolidity/getnowblock", []byte("{}"), &response); err != nil {
		return BlockCutoff{}, err
	}
	if response.BlockID == "" || response.Header.RawData.Timestamp <= 0 {
		return BlockCutoff{}, &CollectionInterruption{Kind: InterruptionUnavailable, Err: errors.New("TronGrid returned a malformed confirmed block")}
	}
	return BlockCutoff{
		BlockID:   response.BlockID,
		Timestamp: time.UnixMilli(response.Header.RawData.Timestamp).UTC(),
	}, nil
}

func (p *TronGridProvider) FetchAddressTransfers(ctx context.Context, request AddressTransferPageRequest) (AddressTransferPage, error) {
	if err := validateTronGridPageRequest(request); err != nil {
		return AddressTransferPage{}, err
	}
	cursor, err := decodeTronGridCursor(request.Cursor)
	if err != nil {
		return AddressTransferPage{}, err
	}
	if cursor.Page == 0 {
		cursor.Page = 1
	}
	if cursor.Page > p.maxPages {
		return AddressTransferPage{}, &CollectionInterruption{Kind: InterruptionResourceLimit, Err: errors.New("TronGrid page limit reached")}
	}
	limit := p.pageLimit
	if request.TransferLimit < limit {
		limit = request.TransferLimit
	}
	query := url.Values{
		"only_confirmed":   {"true"},
		"contract_address": {TRONMainnetUSDTContract},
		"min_timestamp":    {strconv.FormatInt(request.WindowStart.UnixMilli(), 10)},
		"max_timestamp":    {strconv.FormatInt(request.WindowEnd.UnixMilli(), 10)},
		"limit":            {strconv.Itoa(limit)},
	}
	if cursor.Fingerprint != "" {
		query.Set("fingerprint", cursor.Fingerprint)
	}
	path := "/v1/accounts/" + url.PathEscape(request.Address) + "/transactions/trc20?" + query.Encode()
	var response tronGridTRC20Response
	if err := p.requestJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return AddressTransferPage{}, err
	}
	if !response.Success || len(response.Data) > limit || response.Meta.PageSize < 0 || response.Meta.PageSize > limit {
		return AddressTransferPage{}, &CollectionInterruption{Kind: InterruptionUnavailable, Err: errors.New("TronGrid returned a malformed TRC20 page")}
	}

	groups, order := groupTronGridCandidates(response.Data, request)
	transactions := make([]BlockchainTransaction, 0, len(order))
	for _, hash := range order {
		transaction, ok, err := p.verifiedTransaction(ctx, hash, groups[hash], request)
		if err != nil {
			return AddressTransferPage{}, err
		}
		if ok {
			transactions = append(transactions, transaction)
		}
	}
	sort.Slice(transactions, func(i, j int) bool { return transactions[i].Hash < transactions[j].Hash })

	nextCursor := ""
	if response.Meta.Fingerprint != "" {
		nextCursor, err = encodeTronGridCursor(tronGridCursor{Fingerprint: response.Meta.Fingerprint, Page: cursor.Page + 1})
		if err != nil {
			return AddressTransferPage{}, &CollectionInterruption{Kind: InterruptionUnavailable, Err: err}
		}
	}
	return AddressTransferPage{Transactions: transactions, NextCursor: nextCursor}, nil
}

type tronGridTRC20Response struct {
	Data    []tronGridTRC20Record `json:"data"`
	Success bool                  `json:"success"`
	Meta    struct {
		PageSize    int    `json:"page_size"`
		Fingerprint string `json:"fingerprint"`
	} `json:"meta"`
}

type tronGridTRC20Record struct {
	TransactionID string `json:"transaction_id"`
	TokenInfo     struct {
		Symbol   string `json:"symbol"`
		Address  string `json:"address"`
		Decimals int    `json:"decimals"`
	} `json:"token_info"`
	BlockTimestamp int64  `json:"block_timestamp"`
	From           string `json:"from"`
	To             string `json:"to"`
	Type           string `json:"type"`
	Value          string `json:"value"`
}

type tronGridTransactionInfo struct {
	ID             string `json:"id"`
	BlockNumber    int64  `json:"blockNumber"`
	BlockTimestamp int64  `json:"blockTimeStamp"`
	Receipt        struct {
		Result string `json:"result"`
	} `json:"receipt"`
	Logs []struct {
		Address string   `json:"address"`
		Topics  []string `json:"topics"`
		Data    string   `json:"data"`
	} `json:"log"`
}

type tronGridCursor struct {
	Fingerprint string `json:"fingerprint"`
	Page        int    `json:"page"`
}

func validateTronGridPageRequest(request AddressTransferPageRequest) error {
	if request.Network != "TRON_MAINNET" || request.Asset != AssetUSDT || request.CutoffBlockID == "" ||
		request.WindowStart.IsZero() || request.WindowEnd.IsZero() || request.WindowStart.After(request.WindowEnd) ||
		request.TransferLimit < 1 || request.TransferLimit > MaximumTransferLimit ||
		request.TraversalDepth < 1 || request.TraversalDepth > MaximumTraversalDepth || !validTRONAddress(request.Address) {
		return errors.New("invalid TronGrid transfer page request")
	}
	return nil
}

func groupTronGridCandidates(records []tronGridTRC20Record, request AddressTransferPageRequest) (map[string][]tronGridTRC20Record, []string) {
	groups := make(map[string][]tronGridTRC20Record)
	order := make([]string, 0)
	for _, record := range records {
		if !validTronGridCandidate(record, request) {
			continue
		}
		if _, exists := groups[record.TransactionID]; !exists {
			order = append(order, record.TransactionID)
		}
		groups[record.TransactionID] = append(groups[record.TransactionID], record)
	}
	return groups, order
}

func validTronGridCandidate(record tronGridTRC20Record, request AddressTransferPageRequest) bool {
	timestamp := time.UnixMilli(record.BlockTimestamp)
	return validTransactionHash(record.TransactionID) && record.TokenInfo.Symbol == AssetUSDT &&
		record.TokenInfo.Address == TRONMainnetUSDTContract && record.TokenInfo.Decimals == 6 && record.Type == "Transfer" &&
		validTRONAddress(record.From) && validTRONAddress(record.To) && (record.From == request.Address || record.To == request.Address) &&
		unsignedIntegerPattern.MatchString(record.Value) && record.BlockTimestamp > 0 &&
		!timestamp.Before(request.WindowStart) && !timestamp.After(request.WindowEnd)
}

func validTransactionHash(hash string) bool {
	decoded, err := hex.DecodeString(hash)
	return err == nil && len(decoded) == 32
}

func (p *TronGridProvider) verifiedTransaction(ctx context.Context, hash string, candidates []tronGridTRC20Record, request AddressTransferPageRequest) (BlockchainTransaction, bool, error) {
	body, err := json.Marshal(map[string]string{"value": hash})
	if err != nil {
		return BlockchainTransaction{}, false, err
	}
	var info tronGridTransactionInfo
	if err := p.requestJSON(ctx, http.MethodPost, "/walletsolidity/gettransactioninfobyid", body, &info); err != nil {
		return BlockchainTransaction{}, false, err
	}
	blockTime := time.UnixMilli(info.BlockTimestamp).UTC()
	if info.ID == "" && info.BlockNumber == 0 && info.BlockTimestamp == 0 {
		return BlockchainTransaction{}, false, nil
	}
	if info.ID != hash || info.BlockNumber <= 0 || info.BlockTimestamp <= 0 || info.Receipt.Result != "SUCCESS" ||
		blockTime.Before(request.WindowStart) || blockTime.After(request.WindowEnd) {
		return BlockchainTransaction{}, false, nil
	}
	transfers := verifiedReceiptTransfers(info, candidates)
	if len(transfers) == 0 {
		return BlockchainTransaction{}, false, nil
	}
	return BlockchainTransaction{
		Network:        "TRON_MAINNET",
		Hash:           hash,
		BlockID:        strconv.FormatInt(info.BlockNumber, 10),
		BlockTimestamp: blockTime,
		Successful:     true,
		Confirmed:      true,
		Transfers:      transfers,
	}, true, nil
}

func verifiedReceiptTransfers(info tronGridTransactionInfo, candidates []tronGridTRC20Record) []TRC20Transfer {
	// Account history has no event index, so consume matching solidified receipt logs in their canonical array order.
	used := make([]bool, len(candidates))
	transfers := make([]TRC20Transfer, 0, len(candidates))
	for logIndex, log := range info.Logs {
		from, to, amount, ok := decodeUSDTTransferLog(log.Address, log.Topics, log.Data)
		if !ok {
			continue
		}
		for candidateIndex, candidate := range candidates {
			if used[candidateIndex] || candidate.BlockTimestamp != info.BlockTimestamp || candidate.From != from || candidate.To != to || candidate.Value != amount {
				continue
			}
			used[candidateIndex] = true
			transfers = append(transfers, TRC20Transfer{
				EventIdentity:      "log:" + strconv.Itoa(logIndex),
				ContractAddress:    TRONMainnetUSDTContract,
				Asset:              AssetUSDT,
				FromAddress:        from,
				ToAddress:          to,
				AmountSmallestUnit: candidate.Value,
				Decimals:           6,
				Timestamp:          time.UnixMilli(candidate.BlockTimestamp).UTC(),
			})
			break
		}
	}
	return transfers
}

func decodeUSDTTransferLog(address string, topics []string, data string) (string, string, string, bool) {
	address = strings.TrimPrefix(strings.ToLower(address), "0x")
	if strings.HasPrefix(address, "41") && len(address) == 42 {
		address = address[2:]
	}
	if address != tronMainnetUSDTHexAddress || len(topics) != 3 || strings.TrimPrefix(strings.ToLower(topics[0]), "0x") != tronTransferEventTopic {
		return "", "", "", false
	}
	from, ok := tronTopicAddress(topics[1])
	if !ok {
		return "", "", "", false
	}
	to, ok := tronTopicAddress(topics[2])
	if !ok {
		return "", "", "", false
	}
	rawAmount := strings.TrimPrefix(strings.ToLower(data), "0x")
	if len(rawAmount) != 64 {
		return "", "", "", false
	}
	amount, ok := new(big.Int).SetString(rawAmount, 16)
	if !ok {
		return "", "", "", false
	}
	return from, to, amount.String(), true
}

func tronTopicAddress(topic string) (string, bool) {
	topic = strings.TrimPrefix(strings.ToLower(topic), "0x")
	decoded, err := hex.DecodeString(topic)
	if err != nil || len(decoded) != 32 {
		return "", false
	}
	payload := append([]byte{0x41}, decoded[12:]...)
	return encodeTRONAddress(payload), true
}

func validTRONAddress(address string) bool {
	decoded, ok := decodeBase58(address)
	if !ok || len(decoded) != 25 || decoded[0] != 0x41 {
		return false
	}
	firstHash := sha256.Sum256(decoded[:21])
	secondHash := sha256.Sum256(firstHash[:])
	return subtle.ConstantTimeCompare(decoded[21:], secondHash[:4]) == 1
}

func decodeBase58(value string) ([]byte, bool) {
	if value == "" {
		return nil, false
	}
	number := new(big.Int)
	base := big.NewInt(58)
	for i := 0; i < len(value); i++ {
		index := strings.IndexByte(tronBase58Alphabet, value[i])
		if index < 0 {
			return nil, false
		}
		number.Mul(number, base)
		number.Add(number, big.NewInt(int64(index)))
	}
	decoded := number.Bytes()
	for i := 0; i < len(value) && value[i] == '1'; i++ {
		decoded = append([]byte{0}, decoded...)
	}
	return decoded, true
}

func encodeTRONAddress(payload []byte) string {
	firstHash := sha256.Sum256(payload)
	secondHash := sha256.Sum256(firstHash[:])
	full := append(append([]byte{}, payload...), secondHash[:4]...)
	number := new(big.Int).SetBytes(full)
	base := big.NewInt(58)
	zero := new(big.Int)
	var encoded []byte
	for number.Cmp(zero) > 0 {
		quotient, remainder := new(big.Int), new(big.Int)
		quotient.QuoRem(number, base, remainder)
		encoded = append(encoded, tronBase58Alphabet[remainder.Int64()])
		number = quotient
	}
	for left, right := 0, len(encoded)-1; left < right; left, right = left+1, right-1 {
		encoded[left], encoded[right] = encoded[right], encoded[left]
	}
	return string(encoded)
}

func decodeTronGridCursor(value string) (tronGridCursor, error) {
	if value == "" {
		return tronGridCursor{}, nil
	}
	if len(value) > maximumTronGridCursorBytes {
		return tronGridCursor{}, &CollectionInterruption{Kind: InterruptionResourceLimit, Err: errors.New("TronGrid cursor exceeded configured size")}
	}
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return tronGridCursor{}, errors.New("invalid TronGrid cursor")
	}
	var cursor tronGridCursor
	if len(payload) > maximumTronGridCursorBytes || json.Unmarshal(payload, &cursor) != nil || cursor.Fingerprint == "" || cursor.Page < 2 {
		return tronGridCursor{}, errors.New("invalid TronGrid cursor")
	}
	return cursor, nil
}

func encodeTronGridCursor(cursor tronGridCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	if len(payload) > maximumTronGridCursorBytes {
		return "", errors.New("TronGrid cursor exceeded configured size")
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func (p *TronGridProvider) requestJSON(ctx context.Context, method, path string, body []byte, target any) error {
	for attempt := 0; ; attempt++ {
		requestError := p.requestJSONOnce(ctx, method, path, body, target)
		if requestError == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !requestError.retryable || attempt == p.maxRetries {
			return &CollectionInterruption{Kind: requestError.kind, Err: requestError.err}
		}
		delay := p.retryDelay(attempt, requestError)
		if err := sleepWithContext(ctx, delay); err != nil {
			return err
		}
	}
}

type tronGridRequestError struct {
	kind          InterruptionKind
	err           error
	retryable     bool
	retryAfter    time.Duration
	hasRetryAfter bool
}

func (p *TronGridProvider) requestJSONOnce(ctx context.Context, method, path string, body []byte, target any) *tronGridRequestError {
	if err := spendProviderCall(ctx); err != nil {
		return &tronGridRequestError{kind: InterruptionResourceLimit, err: err}
	}
	requestCtx, cancel := context.WithTimeout(ctx, p.httpTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, method, p.baseURL.String()+path, bytes.NewReader(body))
	if err != nil {
		return &tronGridRequestError{kind: InterruptionUnavailable, err: err}
	}
	request.Header.Set(tronGridAPIKeyHeader, p.apiKey)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	response, err := p.httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return &tronGridRequestError{kind: InterruptionUnavailable, err: ctx.Err()}
		}
		return &tronGridRequestError{kind: InterruptionUnavailable, err: err, retryable: true}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		requestError := &tronGridRequestError{
			kind:      InterruptionUnavailable,
			err:       fmt.Errorf("TronGrid HTTP status %d", response.StatusCode),
			retryable: response.StatusCode >= 500 && response.StatusCode <= 599,
		}
		if response.StatusCode == http.StatusTooManyRequests {
			requestError.kind = InterruptionRateLimited
			requestError.retryable = true
			requestError.retryAfter, requestError.hasRetryAfter = parseRetryAfter(response.Header.Get("Retry-After"), time.Now())
		}
		if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusUnauthorized {
			requestError.err = fmt.Errorf("TronGrid rejected the API key (HTTP %d)", response.StatusCode)
			requestError.retryable = false
		}
		return requestError
	}
	limited := io.LimitReader(response.Body, p.maxResponseBytes+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return &tronGridRequestError{kind: InterruptionUnavailable, err: err, retryable: true}
	}
	if int64(len(payload)) > p.maxResponseBytes {
		return &tronGridRequestError{kind: InterruptionResourceLimit, err: errors.New("TronGrid response exceeded configured size")}
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return &tronGridRequestError{kind: InterruptionUnavailable, err: fmt.Errorf("decode TronGrid response: %w", err)}
	}
	return nil
}

func (p *TronGridProvider) retryDelay(attempt int, requestError *tronGridRequestError) time.Duration {
	delay := p.retryBaseDelay
	for i := 0; i < attempt && delay < p.maxRetryDelay; i++ {
		if delay > p.maxRetryDelay/2 {
			delay = p.maxRetryDelay
			break
		}
		delay *= 2
	}
	if requestError.hasRetryAfter {
		delay = requestError.retryAfter
	}
	if delay < 0 {
		return 0
	}
	if delay > p.maxRetryDelay {
		return p.maxRetryDelay
	}
	return delay
}

func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
		return time.Duration(seconds) * time.Second, true
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	return when.Sub(now), true
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
