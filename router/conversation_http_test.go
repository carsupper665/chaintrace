package router_test

import (
	"chaintrace/auth"
	"chaintrace/model"
	"chaintrace/model/store"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

type conversationMessageResponse struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt string `json:"createdAt"`
}

func TestConversationPostPersistsUnavailableOutcome(t *testing.T) {
	test := newAuthHTTPTest(t)
	token, err := auth.GenJWT(test.owner.ID, "")
	if err != nil {
		t.Fatalf("generate owner token: %v", err)
	}
	investigationID := createConversationInvestigation(t, test, token, "Unavailable agent")

	response := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/conversation", map[string]string{
		"idempotencyKey": "command-1",
		"message":        "Trace the suspicious flow",
	}, token)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("conversation POST status = %d, want %d; body: %s", response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
	var got struct {
		Code      string                        `json:"code"`
		Persisted bool                          `json:"persisted"`
		Messages  []conversationMessageResponse `json:"messages"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode unavailable response: %v", err)
	}
	if got.Code != "agent_unavailable" || !got.Persisted {
		t.Errorf("unavailable response = %#v", got)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("persisted messages = %#v, want user and system outcome", got.Messages)
	}
	if got.Messages[0].Role != "user" || got.Messages[0].Content != "Trace the suspicious flow" {
		t.Errorf("user message = %#v", got.Messages[0])
	}
	if got.Messages[1].Role != "system" || got.Messages[1].Content != `{"code":"agent_unavailable"}` {
		t.Errorf("system outcome = %#v", got.Messages[1])
	}
	for _, message := range got.Messages {
		if message.ID == "" || message.CreatedAt == "" {
			t.Errorf("message lacks stable identity/timestamp: %#v", message)
		}
	}

	var saved []store.ConversationMessage
	if err := model.DB.Where("investigation_id = ?", investigationID).Order("ordinal ASC").Find(&saved).Error; err != nil {
		t.Fatalf("read conversation metadata from SQL: %v", err)
	}
	if len(saved) != 2 {
		t.Fatalf("saved conversation metadata = %#v", saved)
	}
	if saved[0].Role != store.ConversationRoleUser || saved[1].Role != store.ConversationRoleSystem ||
		saved[0].IdempotencyKey != "command-1" || saved[1].IdempotencyKey != "command-1" ||
		saved[0].Ordinal != 0 || saved[1].Ordinal != 1 || !saved[0].CreatedAt.Equal(saved[1].CreatedAt) {
		t.Errorf("saved conversation ordering/group = %#v", saved)
	}
	var chunks []store.ConversationChunk
	if err := model.DB.Where("message_id IN ?", []string{saved[0].ID, saved[1].ID}).Order("message_id, chunk_index").Find(&chunks).Error; err != nil {
		t.Fatalf("read conversation chunks from SQL: %v", err)
	}
	if len(chunks) != 2 {
		t.Errorf("saved short-message chunks = %#v, want one per message", chunks)
	}
}

func TestConversationPostRetryReturnsOriginalRecordsWithoutDuplicates(t *testing.T) {
	test := newAuthHTTPTest(t)
	token, err := auth.GenJWT(test.owner.ID, "")
	if err != nil {
		t.Fatalf("generate owner token: %v", err)
	}
	investigationID := createConversationInvestigation(t, test, token, "Idempotent command")
	path := "/api/v1/investigations/" + investigationID + "/conversation"

	first := test.request(http.MethodPost, path, map[string]string{
		"idempotencyKey": "same-command",
		"message":        "original content",
	}, token)
	retry := test.request(http.MethodPost, path, map[string]string{
		"idempotencyKey": "same-command",
		"message":        "changed retry content",
	}, token)
	if first.Code != http.StatusServiceUnavailable || retry.Code != http.StatusServiceUnavailable {
		t.Fatalf("idempotent statuses = (%d, %d); bodies: %s / %s", first.Code, retry.Code, first.Body.String(), retry.Body.String())
	}
	var firstBody, retryBody struct {
		Messages []conversationMessageResponse `json:"messages"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &firstBody); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	if err := json.Unmarshal(retry.Body.Bytes(), &retryBody); err != nil {
		t.Fatalf("decode retry response: %v", err)
	}
	if len(firstBody.Messages) != 2 || len(retryBody.Messages) != 2 {
		t.Fatalf("idempotent messages = %#v / %#v", firstBody.Messages, retryBody.Messages)
	}
	for i := range firstBody.Messages {
		if retryBody.Messages[i] != firstBody.Messages[i] {
			t.Errorf("retry message %d = %#v, want original %#v", i, retryBody.Messages[i], firstBody.Messages[i])
		}
	}
	if retryBody.Messages[0].Content != "original content" {
		t.Errorf("retry replaced original content: %#v", retryBody.Messages[0])
	}
	var messageCount, chunkCount int64
	if err := model.DB.Model(&store.ConversationMessage{}).Where("investigation_id = ?", investigationID).Count(&messageCount).Error; err != nil {
		t.Fatalf("count idempotent messages: %v", err)
	}
	if err := model.DB.Model(&store.ConversationChunk{}).
		Where("message_id IN (SELECT id FROM conversation_messages WHERE investigation_id = ?)", investigationID).
		Count(&chunkCount).Error; err != nil {
		t.Fatalf("count idempotent chunks: %v", err)
	}
	if messageCount != 2 || chunkCount != 2 {
		t.Errorf("idempotent SQL counts = messages %d, chunks %d; want 2, 2", messageCount, chunkCount)
	}
}

func TestConversationLongUnicodeContentUsesExactUTF8SafeChunks(t *testing.T) {
	test := newAuthHTTPTest(t)
	token, err := auth.GenJWT(test.owner.ID, "")
	if err != nil {
		t.Fatalf("generate owner token: %v", err)
	}
	investigationID := createConversationInvestigation(t, test, token, "Unicode chunks")
	message := strings.Repeat("a", 4095) + "界" + strings.Repeat("🙂", 1025) + "tail"

	response := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/conversation", map[string]string{
		"idempotencyKey": "unicode-command",
		"message":        message,
	}, token)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("long Unicode POST = %d; body: %s", response.Code, response.Body.String())
	}
	var body struct {
		Messages []conversationMessageResponse `json:"messages"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode long Unicode response: %v", err)
	}
	if len(body.Messages) != 2 || body.Messages[0].Content != message {
		t.Fatalf("long Unicode response was not exact: message count %d, equal %t", len(body.Messages), len(body.Messages) > 0 && body.Messages[0].Content == message)
	}

	var userRecord store.ConversationMessage
	if err := model.DB.Where("investigation_id = ? AND role = ?", investigationID, store.ConversationRoleUser).First(&userRecord).Error; err != nil {
		t.Fatalf("read Unicode message metadata: %v", err)
	}
	var chunks []store.ConversationChunk
	if err := model.DB.Where("message_id = ?", userRecord.ID).Order("chunk_index ASC").Find(&chunks).Error; err != nil {
		t.Fatalf("read Unicode chunks: %v", err)
	}
	if len(chunks) != 3 {
		t.Fatalf("Unicode chunk count = %d, want 3", len(chunks))
	}
	var rebuilt strings.Builder
	for i, chunk := range chunks {
		if chunk.ChunkIndex != i {
			t.Errorf("chunk %d index = %d", i, chunk.ChunkIndex)
		}
		if len(chunk.Content) > model.ConversationChunkMaxBytes {
			t.Errorf("chunk %d byte length = %d, max %d", i, len(chunk.Content), model.ConversationChunkMaxBytes)
		}
		if !utf8.ValidString(chunk.Content) {
			t.Errorf("chunk %d splits a UTF-8 code point", i)
		}
		rebuilt.WriteString(chunk.Content)
	}
	if rebuilt.String() != message {
		t.Error("ordered SQL chunks did not reconstruct the exact Unicode message")
	}

	reloaded := test.request(http.MethodGet, "/api/v1/investigations/"+investigationID+"/conversation", nil, token)
	if reloaded.Code != http.StatusOK {
		t.Fatalf("reload Unicode conversation = %d; body: %s", reloaded.Code, reloaded.Body.String())
	}
	var page struct {
		Messages []conversationMessageResponse `json:"messages"`
	}
	if err := json.Unmarshal(reloaded.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode reloaded Unicode conversation: %v", err)
	}
	if len(page.Messages) != 2 || page.Messages[0].Content != message {
		t.Error("reloaded conversation did not assemble the exact Unicode content")
	}
}

func TestConversationCursorPagesHaveStableInvestigationBoundOrder(t *testing.T) {
	test := newAuthHTTPTest(t)
	token, err := auth.GenJWT(test.owner.ID, "")
	if err != nil {
		t.Fatalf("generate owner token: %v", err)
	}
	investigationID := createConversationInvestigation(t, test, token, "Cursor pages")
	path := "/api/v1/investigations/" + investigationID + "/conversation"
	for i, message := range []string{"first", "second", "third"} {
		response := test.request(http.MethodPost, path, map[string]string{
			"idempotencyKey": "page-command-" + strconv.Itoa(i),
			"message":        message,
		}, token)
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("seed page command %d = %d; body: %s", i, response.Code, response.Body.String())
		}
	}

	first := test.request(http.MethodGet, path+"?pageSize=2", nil, token)
	repeated := test.request(http.MethodGet, path+"?pageSize=2", nil, token)
	if first.Code != http.StatusOK || repeated.Code != http.StatusOK || first.Body.String() != repeated.Body.String() {
		t.Fatalf("first conversation page is unstable: %d %s / %d %s", first.Code, first.Body.String(), repeated.Code, repeated.Body.String())
	}
	firstPage := decodeConversationPage(t, first.Body.Bytes())
	if len(firstPage.Messages) != 2 || firstPage.NextCursor == nil || *firstPage.NextCursor == "" {
		t.Fatalf("first conversation page boundary = %#v", firstPage)
	}
	if firstPage.Messages[0].Role != "user" || firstPage.Messages[0].Content != "first" || firstPage.Messages[1].Role != "system" {
		t.Errorf("first conversation page order = %#v", firstPage.Messages)
	}
	if strings.Contains(*firstPage.NextCursor, investigationID) {
		t.Errorf("conversation cursor exposes Investigation ID: %q", *firstPage.NextCursor)
	}

	second := test.request(http.MethodGet, path+"?pageSize=2&cursor="+url.QueryEscape(*firstPage.NextCursor), nil, token)
	if second.Code != http.StatusOK {
		t.Fatalf("second conversation page = %d; body: %s", second.Code, second.Body.String())
	}
	secondPage := decodeConversationPage(t, second.Body.Bytes())
	if len(secondPage.Messages) != 2 || secondPage.NextCursor == nil || secondPage.Messages[0].Content != "second" {
		t.Fatalf("second conversation page = %#v", secondPage)
	}
	third := test.request(http.MethodGet, path+"?pageSize=2&cursor="+url.QueryEscape(*secondPage.NextCursor), nil, token)
	if third.Code != http.StatusOK {
		t.Fatalf("third conversation page = %d; body: %s", third.Code, third.Body.String())
	}
	thirdPage := decodeConversationPage(t, third.Body.Bytes())
	if len(thirdPage.Messages) != 2 || thirdPage.NextCursor != nil || thirdPage.Messages[0].Content != "third" {
		t.Fatalf("third conversation page = %#v", thirdPage)
	}

	seen := make(map[string]bool)
	for _, page := range []conversationPageResponse{firstPage, secondPage, thirdPage} {
		for _, message := range page.Messages {
			if seen[message.ID] {
				t.Errorf("message %q appeared in more than one page", message.ID)
			}
			seen[message.ID] = true
		}
	}
	if len(seen) != 6 {
		t.Errorf("paginated unique message count = %d, want 6", len(seen))
	}

	otherInvestigationID := createConversationInvestigation(t, test, token, "Other cursor scope")
	wrongScope := test.request(http.MethodGet, "/api/v1/investigations/"+otherInvestigationID+"/conversation?pageSize=2&cursor="+url.QueryEscape(*firstPage.NextCursor), nil, token)
	if wrongScope.Code != http.StatusBadRequest || !strings.Contains(wrongScope.Body.String(), `"code":"invalid_conversation_cursor"`) {
		t.Errorf("cross-Investigation cursor = %d; body: %s", wrongScope.Code, wrongScope.Body.String())
	}
}

func TestConversationRequiresOwnerAndIsolatesSameKeyBetweenOwners(t *testing.T) {
	test := newAuthHTTPTest(t)
	firstToken, err := auth.GenJWT(test.owner.ID, "")
	if err != nil {
		t.Fatalf("generate first owner token: %v", err)
	}
	secondOwner := store.User{
		Username: "conversation-owner-" + strconv.FormatUint(uint64(test.owner.ID), 10),
		Email:    "conversation-owner-" + strconv.FormatUint(uint64(test.owner.ID), 10) + "@auth-test.invalid",
		Password: "test-only",
		Salt:     "test-only",
	}
	if err := model.DB.Create(&secondOwner).Error; err != nil {
		t.Fatalf("create second conversation owner: %v", err)
	}
	secondToken, err := auth.GenJWT(secondOwner.ID, "")
	if err != nil {
		t.Fatalf("generate second owner token: %v", err)
	}
	firstInvestigationID := createConversationInvestigation(t, test, firstToken, "First owner conversation")
	secondInvestigationID := createConversationInvestigation(t, test, secondToken, "Second owner conversation")
	command := map[string]string{"idempotencyKey": "shared-key", "message": "owner-private content"}

	for _, request := range []struct {
		method string
		body   any
	}{
		{method: http.MethodGet},
		{method: http.MethodPost, body: command},
	} {
		response := test.request(request.method, "/api/v1/investigations/"+firstInvestigationID+"/conversation", request.body, "")
		if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":"unauthorized"`) {
			t.Errorf("unauthenticated %s conversation = %d; body: %s", request.method, response.Code, response.Body.String())
		}
	}

	for _, request := range []struct {
		method string
		body   any
	}{
		{method: http.MethodGet},
		{method: http.MethodPost, body: command},
	} {
		crossOwner := test.request(request.method, "/api/v1/investigations/"+firstInvestigationID+"/conversation", request.body, secondToken)
		missing := test.request(request.method, "/api/v1/investigations/missingConversation00000/conversation", request.body, secondToken)
		if crossOwner.Code != http.StatusNotFound || missing.Code != http.StatusNotFound || crossOwner.Body.String() != missing.Body.String() {
			t.Errorf("%s conversation enumerates ownership: cross %d %s, missing %d %s", request.method, crossOwner.Code, crossOwner.Body.String(), missing.Code, missing.Body.String())
		}
	}

	firstPost := test.request(http.MethodPost, "/api/v1/investigations/"+firstInvestigationID+"/conversation", command, firstToken)
	secondPost := test.request(http.MethodPost, "/api/v1/investigations/"+secondInvestigationID+"/conversation", command, secondToken)
	if firstPost.Code != http.StatusServiceUnavailable || secondPost.Code != http.StatusServiceUnavailable {
		t.Fatalf("same-key owner posts = %d / %d; bodies: %s / %s", firstPost.Code, secondPost.Code, firstPost.Body.String(), secondPost.Body.String())
	}
	for _, investigationID := range []string{firstInvestigationID, secondInvestigationID} {
		var count int64
		if err := model.DB.Model(&store.ConversationMessage{}).
			Where("investigation_id = ? AND idempotency_key = ?", investigationID, "shared-key").Count(&count).Error; err != nil {
			t.Fatalf("count owner-scoped idempotency group: %v", err)
		}
		if count != 2 {
			t.Errorf("Investigation %s same-key count = %d, want 2", investigationID, count)
		}
	}
}

func TestConcurrentConversationPostsWithSameKeyCreateOnePair(t *testing.T) {
	test := newAuthHTTPTest(t)
	token, err := auth.GenJWT(test.owner.ID, "")
	if err != nil {
		t.Fatalf("generate owner token: %v", err)
	}
	investigationID := createConversationInvestigation(t, test, token, "Concurrent idempotency")
	path := "/api/v1/investigations/" + investigationID + "/conversation"

	const attempts = 12
	type result struct {
		status   int
		messages []conversationMessageResponse
		body     string
	}
	results := make(chan result, attempts)
	start := make(chan struct{})
	var wait sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wait.Add(1)
		go func(attempt int) {
			defer wait.Done()
			<-start
			response := test.request(http.MethodPost, path, map[string]string{
				"idempotencyKey": "concurrent-key",
				"message":        "attempt-" + strconv.Itoa(attempt),
			}, token)
			var decoded struct {
				Messages []conversationMessageResponse `json:"messages"`
			}
			_ = json.Unmarshal(response.Body.Bytes(), &decoded)
			results <- result{status: response.Code, messages: decoded.Messages, body: response.Body.String()}
		}(i)
	}
	close(start)
	wait.Wait()
	close(results)

	var winner []conversationMessageResponse
	for result := range results {
		if result.status != http.StatusServiceUnavailable || len(result.messages) != 2 {
			t.Errorf("concurrent response = %d; body: %s", result.status, result.body)
			continue
		}
		if winner == nil {
			winner = result.messages
			continue
		}
		for i := range winner {
			if result.messages[i] != winner[i] {
				t.Errorf("concurrent response differs at message %d: %#v, want %#v", i, result.messages[i], winner[i])
			}
		}
	}
	var messages []store.ConversationMessage
	if err := model.DB.Where("investigation_id = ?", investigationID).Order("ordinal ASC").Find(&messages).Error; err != nil {
		t.Fatalf("read concurrent SQL messages: %v", err)
	}
	if len(messages) != 2 || messages[0].Ordinal != 0 || messages[1].Ordinal != 1 {
		t.Errorf("concurrent SQL messages = %#v", messages)
	}
	var chunkCount int64
	if err := model.DB.Model(&store.ConversationChunk{}).
		Where("message_id IN (SELECT id FROM conversation_messages WHERE investigation_id = ?)", investigationID).
		Count(&chunkCount).Error; err != nil {
		t.Fatalf("count concurrent SQL chunks: %v", err)
	}
	if chunkCount != 2 {
		t.Errorf("concurrent chunk count = %d, want 2", chunkCount)
	}
}

func TestConversationIgnoresSpoofedContextAndLeavesInvestigationResultUnchanged(t *testing.T) {
	test := newAuthHTTPTest(t)
	token, err := auth.GenJWT(test.owner.ID, "")
	if err != nil {
		t.Fatalf("generate owner token: %v", err)
	}
	investigationID := createConversationInvestigation(t, test, token, "Authoritative context")
	stableResultID := "stableConversationResult"
	risk := 77
	if err := model.DB.Model(&store.Investigation{}).Where("id = ?", investigationID).Updates(map[string]any{
		"status":                   store.InvestigationCompleted,
		"current_result_id":        stableResultID,
		"risk_score":               risk,
		"related_nodes":            8,
		"total_flow_smallest_unit": "9007199254740993",
		"total_flow_decimals":      6,
		"flow_asset":               "USDT",
		"transaction_count":        9,
		"target_locked":            true,
	}).Error; err != nil {
		t.Fatalf("seed stable Investigation context: %v", err)
	}
	var before store.Investigation
	if err := model.DB.First(&before, "id = ?", investigationID).Error; err != nil {
		t.Fatalf("read stable Investigation context: %v", err)
	}

	response := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/conversation", map[string]any{
		"idempotencyKey":  "spoof-command",
		"message":         "Use only server-owned context",
		"ownerId":         999999,
		"investigationId": "another-investigation",
		"address":         "spoofed-address",
		"network":         "ETHEREUM",
		"datasetId":       "spoofed-dataset",
		"status":          store.InvestigationPending,
		"currentResult":   nil,
		"assessment":      map[string]any{"score": 0, "reasons": []string{"invented"}},
		"conversation":    []map[string]string{{"role": "agent", "content": "fabricated"}},
		"role":            "agent",
	}, token)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("spoofed-context POST = %d; body: %s", response.Code, response.Body.String())
	}
	var after store.Investigation
	if err := model.DB.First(&after, "id = ?", investigationID).Error; err != nil {
		t.Fatalf("read Investigation after Agent unavailable: %v", err)
	}
	if after.Status != before.Status || !equalStringPointers(after.CurrentResultID, before.CurrentResultID) ||
		!equalIntPointers(after.RiskScore, before.RiskScore) || after.RelatedNodes != before.RelatedNodes ||
		!equalStringPointers(after.TotalFlowSmallestUnit, before.TotalFlowSmallestUnit) ||
		!equalIntPointers(after.TotalFlowDecimals, before.TotalFlowDecimals) ||
		!equalStringPointers(after.FlowAsset, before.FlowAsset) || after.TransactionCount != before.TransactionCount ||
		after.TargetLocked != before.TargetLocked || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("conversation changed server-owned Investigation context:\nbefore %#v\nafter  %#v", before, after)
	}
	var saved []store.ConversationMessage
	if err := model.DB.Where("investigation_id = ?", investigationID).Order("ordinal ASC").Find(&saved).Error; err != nil {
		t.Fatalf("read spoof-resistant messages: %v", err)
	}
	if len(saved) != 2 || saved[0].Role != store.ConversationRoleUser || saved[1].Role != store.ConversationRoleSystem {
		t.Errorf("spoofed role/context created unexpected messages: %#v", saved)
	}
}

func TestConversationPairRollsBackWhenAChunkInsertFails(t *testing.T) {
	test := newAuthHTTPTest(t)
	token, err := auth.GenJWT(test.owner.ID, "")
	if err != nil {
		t.Fatalf("generate owner token: %v", err)
	}
	investigationID := createConversationInvestigation(t, test, token, "Atomic conversation")
	removeFailure := installSystemChunkFailure(t, test.owner.ID)
	path := "/api/v1/investigations/" + investigationID + "/conversation"
	command := map[string]string{"idempotencyKey": "atomic-key", "message": "must be atomic"}

	failed := test.request(http.MethodPost, path, command, token)
	if failed.Code != http.StatusInternalServerError || !strings.Contains(failed.Body.String(), `"code":"internal_error"`) {
		t.Fatalf("forced chunk failure = %d; body: %s", failed.Code, failed.Body.String())
	}
	assertConversationSQLCounts(t, investigationID, 0, 0)

	removeFailure()
	retry := test.request(http.MethodPost, path, command, token)
	if retry.Code != http.StatusServiceUnavailable {
		t.Fatalf("retry after rollback = %d; body: %s", retry.Code, retry.Body.String())
	}
	assertConversationSQLCounts(t, investigationID, 2, 2)
	var saved []store.ConversationMessage
	if err := model.DB.Where("investigation_id = ?", investigationID).Order("ordinal ASC").Find(&saved).Error; err != nil {
		t.Fatalf("read messages after rollback retry: %v", err)
	}
	if len(saved) != 2 || saved[0].Ordinal != 0 || saved[1].Ordinal != 1 {
		t.Errorf("rollback consumed conversation ordering: %#v", saved)
	}
}

func TestDeletingInvestigationCascadesConversationMessagesAndChunks(t *testing.T) {
	test := newAuthHTTPTest(t)
	token, err := auth.GenJWT(test.owner.ID, "")
	if err != nil {
		t.Fatalf("generate owner token: %v", err)
	}
	investigationID := createConversationInvestigation(t, test, token, "Cascade conversation")
	path := "/api/v1/investigations/" + investigationID
	response := test.request(http.MethodPost, path+"/conversation", map[string]string{
		"idempotencyKey": "cascade-key",
		"message":        strings.Repeat("界", 3000),
	}, token)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("seed cascade conversation = %d; body: %s", response.Code, response.Body.String())
	}
	var messages []store.ConversationMessage
	if err := model.DB.Where("investigation_id = ?", investigationID).Find(&messages).Error; err != nil {
		t.Fatalf("read cascade message IDs: %v", err)
	}
	messageIDs := make([]string, 0, len(messages))
	for _, message := range messages {
		messageIDs = append(messageIDs, message.ID)
	}
	var chunksBefore int64
	if err := model.DB.Model(&store.ConversationChunk{}).Where("message_id IN ?", messageIDs).Count(&chunksBefore).Error; err != nil {
		t.Fatalf("count chunks before cascade: %v", err)
	}
	if len(messages) != 2 || chunksBefore < 4 {
		t.Fatalf("cascade fixture = %d messages, %d chunks", len(messages), chunksBefore)
	}

	deleted := test.request(http.MethodDelete, path, nil, token)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete Investigation with conversation = %d; body: %s", deleted.Code, deleted.Body.String())
	}
	var messagesAfter, chunksAfter int64
	if err := model.DB.Model(&store.ConversationMessage{}).Where("id IN ?", messageIDs).Count(&messagesAfter).Error; err != nil {
		t.Fatalf("count messages after cascade: %v", err)
	}
	if err := model.DB.Model(&store.ConversationChunk{}).Where("message_id IN ?", messageIDs).Count(&chunksAfter).Error; err != nil {
		t.Fatalf("count chunks after cascade: %v", err)
	}
	if messagesAfter != 0 || chunksAfter != 0 {
		t.Errorf("cascade left messages/chunks = %d/%d", messagesAfter, chunksAfter)
	}
	reload := test.request(http.MethodGet, path+"/conversation", nil, token)
	if reload.Code != http.StatusNotFound || !strings.Contains(reload.Body.String(), `"code":"investigation_not_found"`) {
		t.Errorf("deleted conversation reload = %d; body: %s", reload.Code, reload.Body.String())
	}
}

func TestConversationEnforcesMessageRequestAndPageBounds(t *testing.T) {
	test := newAuthHTTPTest(t)
	token, err := auth.GenJWT(test.owner.ID, "")
	if err != nil {
		t.Fatalf("generate owner token: %v", err)
	}
	investigationID := createConversationInvestigation(t, test, token, "Conversation bounds")
	path := "/api/v1/investigations/" + investigationID + "/conversation"

	empty := test.request(http.MethodGet, path, nil, token)
	if empty.Code != http.StatusOK || strings.TrimSpace(empty.Body.String()) != `{"messages":[],"nextCursor":null}` {
		t.Errorf("default empty conversation page = %d; body: %s", empty.Code, empty.Body.String())
	}
	maximumPage := test.request(http.MethodGet, path+"?pageSize=100", nil, token)
	if maximumPage.Code != http.StatusOK {
		t.Errorf("maximum conversation page = %d; body: %s", maximumPage.Code, maximumPage.Body.String())
	}
	for _, query := range []string{"?pageSize=0", "?pageSize=101", "?pageSize=not-a-number", "?pageSize=1&pageSize=2"} {
		response := test.request(http.MethodGet, path+query, nil, token)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"invalid_conversation_query"`) {
			t.Errorf("invalid page query %q = %d; body: %s", query, response.Code, response.Body.String())
		}
	}
	for _, query := range []string{"?cursor=", "?cursor=not-a-cursor"} {
		response := test.request(http.MethodGet, path+query, nil, token)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"invalid_conversation_cursor"`) {
			t.Errorf("invalid cursor query %q = %d; body: %s", query, response.Code, response.Body.String())
		}
	}

	invalidBodies := []map[string]any{
		{"idempotencyKey": "", "message": "message"},
		{"idempotencyKey": "key", "message": "   "},
		{"idempotencyKey": strings.Repeat("k", 129), "message": "message"},
		{"idempotencyKey": "too-long-message", "message": strings.Repeat("x", (256<<10)+1)},
		{"idempotencyKey": "too-large-request", "message": "message", "padding": strings.Repeat("x", (1<<20)+1)},
	}
	for i, body := range invalidBodies {
		response := test.request(http.MethodPost, path, body, token)
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"validation_error"`) {
			t.Errorf("invalid conversation body %d = %d; body: %s", i, response.Code, response.Body.String())
		}
	}
	assertConversationSQLCounts(t, investigationID, 0, 0)
}

func TestConversationSQLConstraintsRejectDuplicateKeysAndInvalidRoles(t *testing.T) {
	test := newAuthHTTPTest(t)
	token, err := auth.GenJWT(test.owner.ID, "")
	if err != nil {
		t.Fatalf("generate owner token: %v", err)
	}
	investigationID := createConversationInvestigation(t, test, token, "Conversation constraints")
	response := test.request(http.MethodPost, "/api/v1/investigations/"+investigationID+"/conversation", map[string]string{
		"idempotencyKey": "constraint-key",
		"message":        "constraint fixture",
	}, token)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("seed constrained conversation = %d; body: %s", response.Code, response.Body.String())
	}
	var userMessage store.ConversationMessage
	if err := model.DB.Where("investigation_id = ? AND role = ?", investigationID, store.ConversationRoleUser).First(&userMessage).Error; err != nil {
		t.Fatalf("read constrained user message: %v", err)
	}

	duplicateChunk := store.ConversationChunk{MessageID: userMessage.ID, ChunkIndex: 0, Content: "duplicate"}
	if err := model.DB.Create(&duplicateChunk).Error; err == nil {
		t.Error("SQL accepted duplicate (message_id, chunk_index)")
	}
	duplicateKeyRole := store.ConversationMessage{
		ID:              strings.Repeat("d", 24),
		InvestigationID: investigationID,
		Role:            store.ConversationRoleUser,
		IdempotencyKey:  userMessage.IdempotencyKey,
		Ordinal:         100,
		CreatedAt:       userMessage.CreatedAt,
	}
	if err := model.DB.Create(&duplicateKeyRole).Error; err == nil {
		t.Error("SQL accepted duplicate Investigation/idempotency-key/role")
	}
	invalidRole := store.ConversationMessage{
		ID:              strings.Repeat("r", 24),
		InvestigationID: investigationID,
		Role:            store.ConversationRole("assistant"),
		IdempotencyKey:  "invalid-role-key",
		Ordinal:         101,
		CreatedAt:       userMessage.CreatedAt,
	}
	if err := model.DB.Create(&invalidRole).Error; err == nil {
		t.Error("SQL accepted a conversation role outside user/system/agent")
	}
	assertConversationSQLCounts(t, investigationID, 2, 2)
}

func installSystemChunkFailure(t *testing.T, ownerID uint) func() {
	t.Helper()
	suffix := strconv.FormatUint(uint64(ownerID), 10)
	triggerName := "fail_system_chunk_" + suffix
	removed := false
	var cleanup func()
	switch model.DB.Dialector.Name() {
	case "postgres":
		functionName := "fail_system_chunk_fn_" + suffix
		if err := model.DB.Exec(fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger AS $$
BEGIN
  IF (SELECT role FROM conversation_messages WHERE id = NEW.message_id) = 'system' THEN
    RAISE EXCEPTION 'forced conversation chunk failure';
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql`, functionName)).Error; err != nil {
			t.Fatalf("create PostgreSQL chunk failure function: %v", err)
		}
		if err := model.DB.Exec(fmt.Sprintf("CREATE TRIGGER %s BEFORE INSERT ON conversation_chunks FOR EACH ROW EXECUTE FUNCTION %s()", triggerName, functionName)).Error; err != nil {
			_ = model.DB.Exec(fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", functionName)).Error
			t.Fatalf("create PostgreSQL chunk failure trigger: %v", err)
		}
		cleanup = func() {
			_ = model.DB.Exec(fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON conversation_chunks", triggerName)).Error
			_ = model.DB.Exec(fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", functionName)).Error
		}
	default:
		statement := fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON conversation_chunks
WHEN (SELECT role FROM conversation_messages WHERE id = NEW.message_id) = 'system'
BEGIN
  SELECT RAISE(FAIL, 'forced conversation chunk failure');
END`, triggerName)
		if err := model.DB.Exec(statement).Error; err != nil {
			t.Fatalf("create SQLite chunk failure trigger: %v", err)
		}
		cleanup = func() {
			_ = model.DB.Exec(fmt.Sprintf("DROP TRIGGER IF EXISTS %s", triggerName)).Error
		}
	}
	t.Cleanup(func() {
		if !removed {
			cleanup()
		}
	})
	return func() {
		if !removed {
			cleanup()
			removed = true
		}
	}
}

func assertConversationSQLCounts(t *testing.T, investigationID string, wantMessages, wantChunks int64) {
	t.Helper()
	var messageCount, chunkCount int64
	if err := model.DB.Model(&store.ConversationMessage{}).Where("investigation_id = ?", investigationID).Count(&messageCount).Error; err != nil {
		t.Fatalf("count conversation messages: %v", err)
	}
	if err := model.DB.Model(&store.ConversationChunk{}).
		Where("message_id IN (SELECT id FROM conversation_messages WHERE investigation_id = ?)", investigationID).
		Count(&chunkCount).Error; err != nil {
		t.Fatalf("count conversation chunks: %v", err)
	}
	if messageCount != wantMessages || chunkCount != wantChunks {
		t.Errorf("conversation SQL counts = messages %d, chunks %d; want %d, %d", messageCount, chunkCount, wantMessages, wantChunks)
	}
}

func equalStringPointers(left, right *string) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func equalIntPointers(left, right *int) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

type conversationPageResponse struct {
	Messages   []conversationMessageResponse `json:"messages"`
	NextCursor *string                       `json:"nextCursor"`
}

func decodeConversationPage(t *testing.T, body []byte) conversationPageResponse {
	t.Helper()
	var page conversationPageResponse
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatalf("decode conversation page: %v; body: %s", err, body)
	}
	return page
}

func createConversationInvestigation(t *testing.T, test *authHTTPTest, token, title string) string {
	t.Helper()
	response := test.request(http.MethodPost, "/api/v1/investigations", map[string]string{"title": title}, token)
	if response.Code != http.StatusCreated {
		t.Fatalf("create conversation investigation = %d; body: %s", response.Code, response.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode conversation investigation: %v", err)
	}
	return created.ID
}
