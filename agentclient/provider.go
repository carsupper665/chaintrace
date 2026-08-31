package agentclient

import (
	"context"
	"errors"
	"fmt"

	"chaintrace/analysis"
	"chaintrace/controller"
	"chaintrace/model"
	"chaintrace/model/store"
)

const (
	datasetCacheSize  = 32
	evidenceCacheSize = 64
)

// Provider implements controller.AgentProvider by talking to the Python
// sidecar and running the tools it asks for.
type Provider struct {
	options   Options
	transport *transport
	// runs lets the Agent start an Analysis Run and read one in flight. Without
	// it the Agent can still read and explain, but not investigate on its own.
	runs     *analysis.RunManager
	datasets *lruCache[*datasetView]
	evidence *lruCache[*EvidenceAnalysis]
}

// New returns nil when no Agent is configured, which keeps the existing
// agent_unavailable behaviour rather than failing at startup.
func New(options Options, runs *analysis.RunManager) *Provider {
	if !options.Enabled() {
		return nil
	}
	options = options.withDefaults()
	return &Provider{
		options:   options,
		transport: newTransport(options),
		runs:      runs,
		datasets:  newLRUCache[*datasetView](datasetCacheSize),
		evidence:  newLRUCache[*EvidenceAnalysis](evidenceCacheSize),
	}
}

// Respond runs one turn: resolve authorization once, hand the Agent the
// evidence, then execute whatever Tier 1 tools it asks for until it answers or
// the budget runs out.
func (p *Provider) Respond(
	ctx context.Context, request controller.AgentRequest,
) (controller.AgentResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, p.options.TurnTimeout)
	defer cancel()

	// Check 1: the scope is derived here, from this process's own database.
	// Nothing the Agent later says about ownership can widen it.
	scope, err := p.resolveScope(request.OwnerID, request.InvestigationID)
	if err != nil {
		return controller.AgentResponse{}, err
	}
	evidence := p.evidenceFor(scope)

	mode := string(request.Mode)
	if mode == "" {
		mode = string(controller.AgentModeChat)
	}
	text, err := p.runTurn(ctx, scope, chatRequest{
		SessionID: request.InvestigationID,
		// A session is scoped to the evidence it was opened on. Before any
		// analysis exists the key is the Investigation itself, so starting a
		// run later invalidates the session and the Agent sees the new result.
		DatasetID: scope.sessionKey(),
		Mode:      mode,
		Evidence:  &evidence,
		Messages:  wireMessages(request.Conversation),
		Tools:     ToolSpecs(),
	})
	if err != nil {
		return controller.AgentResponse{}, err
	}
	return controller.AgentResponse{Content: text}, nil
}

// runTurn drives the tool loop. It takes an already-resolved scope so the loop
// itself can be tested without a database.
func (p *Provider) runTurn(
	ctx context.Context, scope turnScope, opening chatRequest,
) (string, error) {
	runner := toolRunner{options: p.options, runs: p.runs}
	reply, err := p.transport.chat(ctx, opening)
	if err != nil {
		return "", err
	}

	// Check 4a: a hard ceiling on tool calls per turn.
	for calls := 0; len(reply.ToolCalls) > 0 && calls < p.options.MaxToolCalls; calls++ {
		// Check 4b: the turn deadline wins over any pending tool work.
		if ctx.Err() != nil {
			break
		}
		results := make([]ToolResult, 0, len(reply.ToolCalls))
		for _, call := range reply.ToolCalls {
			results = append(results, runner.execute(scope, call))
		}
		reply, err = p.continueTurn(ctx, opening, results)
		if err != nil {
			return "", err
		}
	}

	if reply.Text == "" {
		// Out of tool budget with nothing written, or an empty completion.
		// Saying so is better than storing a blank agent message.
		return "", fmt.Errorf("agent produced no answer")
	}
	return reply.Text, nil
}

// continueTurn feeds tool results back. If the sidecar has lost the session —
// it restarted, or the entry aged out — the turn is restarted once from the
// opening payload. Tool work done so far is discarded rather than replayed:
// replaying would need the Agent's own tool-call turn, which the wire format
// deliberately does not carry.
func (p *Provider) continueTurn(
	ctx context.Context, opening chatRequest, results []ToolResult,
) (chatResponse, error) {
	reply, err := p.transport.chat(ctx, chatRequest{
		SessionID:   opening.SessionID,
		DatasetID:   opening.DatasetID,
		Mode:        opening.Mode,
		ToolResults: results,
	})
	if !errors.Is(err, errSessionExpired) {
		return reply, err
	}
	return p.transport.chat(ctx, opening)
}

// resolveScope performs the turn's single authorization lookup.
//
// An Investigation with no published analysis resolves successfully with an
// empty dataset view: a brand new Investigation is something the Agent can
// talk about and act on, not an error (ADR-0013).
func (p *Provider) resolveScope(ownerID uint, investigationID string) (turnScope, error) {
	var investigation store.Investigation
	if err := model.DB.Where("owner_id = ? AND id = ?", ownerID, investigationID).
		First(&investigation).Error; err != nil {
		return turnScope{}, err
	}

	target := ""
	if investigation.Address != nil {
		target = *investigation.Address
	}
	scope := turnScope{
		ownerID:         ownerID,
		investigationID: investigationID,
		investigation:   investigation,
		target:          target,
		network:         investigation.Network,
		view:            newDatasetView("", nil),
		assessments:     map[string]analysis.NodeAssessment{},
	}

	current, err := analysis.LoadCurrentResult(ownerID, investigationID)
	if errors.Is(err, analysis.ErrCurrentResultNotFound) {
		return scope, nil
	}
	if err != nil {
		return turnScope{}, err
	}

	view, err := p.datasetView(current.Dataset.ID)
	if err != nil {
		return turnScope{}, err
	}
	for _, assessment := range current.Assessment.NodeAssessments {
		scope.assessments[assessment.Address] = assessment
	}
	scope.datasetID = current.Dataset.ID
	scope.view = view
	scope.current = &current
	return scope, nil
}

// datasetView and evidenceFor are cached by dataset id. A dataset never
// changes, so a hit can never be stale.
func (p *Provider) datasetView(datasetID string) (*datasetView, error) {
	if cached, ok := p.datasets.Get(datasetID); ok {
		return cached, nil
	}
	view, err := loadDatasetView(datasetID)
	if err != nil {
		return nil, err
	}
	p.datasets.Put(datasetID, view)
	return view, nil
}

// evidenceFor assembles the turn's evidence. Only the analysis half is cached,
// because only a dataset is immutable — the Investigation's own state and any
// run in flight change from turn to turn.
func (p *Provider) evidenceFor(scope turnScope) Evidence {
	evidence := Evidence{
		Investigation: describeInvestigation(scope.investigation),
		ActiveRun:     p.activeRun(scope),
	}
	if scope.current == nil {
		return evidence
	}
	if cached, ok := p.evidence.Get(scope.datasetID); ok {
		evidence.Analysis = cached
		return evidence
	}
	analysisEvidence := buildAnalysisEvidence(*scope.current, scope.target, scope.view)
	p.evidence.Put(scope.datasetID, analysisEvidence)
	evidence.Analysis = analysisEvidence
	return evidence
}

func (p *Provider) activeRun(scope turnScope) *EvidenceRun {
	runID := scope.investigation.ActiveAnalysisRunID
	if p.runs == nil || runID == nil || *runID == "" {
		return nil
	}
	run, err := p.runs.Get(scope.ownerID, scope.investigationID, *runID)
	if err != nil {
		return nil
	}
	described := &EvidenceRun{
		ID:                 run.ID,
		Status:             run.Status,
		Phase:              run.Phase,
		CollectedTransfers: run.CollectedTransfers,
	}
	if run.ErrorCode != nil {
		described.ErrorCode = *run.ErrorCode
	}
	return described
}

// wireMessages converts stored conversation into what the Agent sees. System
// messages hold machine outcomes such as {"code":"agent_unavailable"} and are
// dropped: they are records for the UI, not turns in a conversation.
func wireMessages(messages []controller.AgentConversationMessage) []wireMessage {
	result := make([]wireMessage, 0, len(messages))
	for _, message := range messages {
		switch message.Role {
		case store.ConversationRoleUser:
			result = append(result, wireMessage{Role: "user", Content: message.Content})
		case store.ConversationRoleAgent:
			result = append(result, wireMessage{Role: "assistant", Content: message.Content})
		}
	}
	return result
}
