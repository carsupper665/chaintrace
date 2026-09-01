# ChainTrace Investigation

ChainTrace supports blockchain investigations, their transaction relationships, assessments, and analyst-agent interactions.

## Language

**Investigation**:
A tracked blockchain inquiry belonging to one Owner, with an identity, title, address, network, risk summary, aggregate transaction metrics, and lifecycle status.
_Avoid_: Case

**Owner**:
The authenticated user who controls and may access an Investigation and all records belonging to it.
_Avoid_: Viewer, session user

**Investigation Session**:
The working context of one Investigation, containing its current graph, analysis, conversation, and in-progress interaction state. It shares the Investigation's identity rather than having an independent lifecycle.
_Avoid_: Agent session

**Investigation Status**:
The analysis lifecycle of an Investigation. `待處理` has no completed analysis, `分析中` has a current analysis run in progress, and `已完成` has a saved current analysis result; completion does not close the Investigation.

**Agent Turn**:
One exchange within an Investigation Session: the Owner's message and the Agent's answer, together with any Tool Calls run in between. A turn always ends with an answer and never waits for an Analysis Run to finish.
_Avoid_: Agent task, investigation run

**Tool Call**:
The Agent's request for one investigation operation to be carried out on its behalf. The Agent asks; the backend decides whether the request is within the Owner's Analysis Scope and carries it out.
_Avoid_: Function call, agent action

**Agent Evidence**:
The bounded set of facts an Agent Turn may reason from. Anything outside it is unknown to the Agent, and an Agent may not assert what the Evidence cannot support.
_Avoid_: Context, prompt data

**Network**:
The blockchain network on which an Investigation's address and transactions are interpreted. The same address text on different Networks does not identify the same investigation target.
_Avoid_: Chain

**TRC20 Transfer**:
A token movement event produced by a TRC20 contract on the TRON Network. Each event belongs to one Blockchain Transaction and forms one transaction relationship in the Transaction Graph.
_Avoid_: TRC20 chain, TRON transaction

**Blockchain Transaction**:
An on-chain action included in a block that may produce zero or more TRC20 Transfers. It is the provenance record for those Transfers rather than the fund-flow relationship itself.
_Avoid_: Transfer, graph edge

**Eligible Transfer**:
A TRC20 Transfer from a successful, confirmed Blockchain Transaction that may be admitted to an Analysis Dataset.
_Avoid_: Pending transfer, failed transfer

**Asset**:
The token whose value moves in a transaction relationship. The first MVP Asset is TRC20 USDT on TRON mainnet.
_Avoid_: Network, coin

**Transfer Amount**:
The exact quantity moved by a TRC20 Transfer, represented in the Asset's smallest unit together with its decimal scale.
_Avoid_: Floating-point value

**Investigation Metrics**:
The aggregate related-address count, Transfer count, total flow, and Asset derived from one Analysis Dataset.
_Avoid_: View metrics

**Investigation Target**:
The pair of address and Network examined by an Investigation. It may be edited while the Investigation is pending and becomes fixed after the first analysis completes.
_Avoid_: Focus address, target wallet

**Transaction Graph**:
The relationship representation of an Analysis Dataset, including aggregate transaction measures but excluding visual layout.
_Avoid_: Graph view, diagram

**Graph Expansion**:
The disclosure of additional relationships already present in an Investigation's Analysis Dataset. It changes the visible graph but not the evidence being analyzed.

**Analysis Scope**:
The explicit boundary that determines which blockchain evidence qualifies for an Investigation's analysis.
_Avoid_: Search settings, graph view

**Analysis Window**:
The fixed 30-day period ending at the confirmed block cutoff captured when an analysis begins.
_Avoid_: Date filter

**Analysis Dataset**:
The normalized blockchain evidence collected for an Investigation Target within one Analysis Scope. It is the shared basis for graph data, aggregate metrics, and assessments regardless of how much the frontend renders.
_Avoid_: Visible graph

**Transfer Limit**:
The requested upper bound on unique Eligible Transfers admitted to an Analysis Dataset. A default applies when none is requested, and no request may exceed the system maximum.
_Avoid_: Transaction limit, page size, render limit

**Traversal Depth**:
The maximum number of transaction-relationship hops followed outward from an Investigation Target while building an Analysis Dataset.
_Avoid_: Graph depth, render depth

**Analysis Coverage**:
The account of how many Eligible Transfers were requested and collected, together with why collection stopped. Coverage describes the bounded dataset and never claims that all blockchain history was analyzed.
_Avoid_: Completeness

**Partial Analysis**:
An assessment produced when evidence collection stops unexpectedly before satisfying its Analysis Scope. It remains usable only when accompanied by Analysis Coverage, a stop reason, and reduced confidence.
_Avoid_: Complete analysis

**Analysis Run**:
One asynchronous attempt to collect an Analysis Dataset and produce assessments for an Investigation. Its status is `queued`, `running`, `completed`, `failed`, or `cancelled`; a cancelled run publishes no Dataset or assessment, and an Investigation may have only one queued or running Analysis Run at a time.
_Avoid_: Investigation, Agent Turn

**Risk Score**:
A 0-to-100 assessment derived from one Analysis Dataset. It is interpreted together with Analysis Coverage and confidence rather than as an unconditional property of an address.
_Avoid_: Address rating

**Anomaly Analysis**:
The structured assessment of an Analysis Dataset, comprising its Risk Score, severity level, contributing reasons, and address-level assessments.
_Avoid_: LLM opinion

**Address Feature Vector**:
The fixed set of numeric measures computed for one address from its own Transfers over a stated window and transfer cap. The same computation serves model training and live scoring, so a vector is comparable only to vectors built under the same window and cap.
_Avoid_: Metrics, statistics

**Learned Risk Score**:
An anomaly score produced for an Investigation Target by a trained model from its Address Feature Vector. It says how unlike ordinary USDT activity the address looks; it carries no reasons and is not the published Risk Score.
_Avoid_: Risk Score, AI score

**Interpretation**:
A human-readable explanation or recommendation derived from an Anomaly Analysis. It may be generated by an agent but cannot change the underlying score or evidence.
_Avoid_: Assessment, evidence
