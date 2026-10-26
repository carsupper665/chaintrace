# Screenshot Claim extraction is a one-shot Agent sidecar route

Owners often hold a screenshot rather than a typed TRON address. Extracting a
structured Claim from that image needs a vision-capable LLM, but Claims are not
Agent Evidence, must not enter conversation or an Analysis Dataset, and must be
re-validated by Go before any Investigation Target is prefilled.

The chat/tool-loop Agent is the wrong surface: it is Investigation-scoped, text
first, and may request tools. Claim extraction is therefore a separate
`POST /v1/extract-claims` route on the same Python sidecar, mirroring
`POST /v1/score` (ADR-0015): one request, structured JSON out, no tools, shared
`X-Agent-Key` only. Go owns Owner auth, PNG intake, Base58Check and on-chain
activity checks through the existing ChainDataProvider, and never persists the
image. The frontend BFF proxies authenticated multipart uploads and never calls
Python directly.

## Consequences

Vendor multimodal encoding stays inside LLM adapters; the neutral `Message`
contract may carry image bytes. Adapters that cannot accept images fail closed.
Verified addresses only prefill the existing Investigation Target field; no
second create API is introduced.
