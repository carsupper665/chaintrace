# FGF login preserves local Owner identity

Status: accepted

The browser may start FGF OIDC login through the BFF. The Go API owns the OIDC client secret, validates the authorization code using the shared fgf-oidc package and resolves an Owner using a stable issuer/subject key. The BFF transfers the resulting local credential into its existing HttpOnly cookie; domain requests keep their existing Owner authentication and authorization.

A new external identity creates an ordinary role-1 Owner. Matching email alone never links an external identity to an existing Owner. Explicit administrative linking is needed for existing accounts, preserving their Investigation ownership and service-specific privileges. IDP administrative roles confer no service privileges.

The existing password/email challenge remains available. FGF changes how the Owner authenticates, not the Investigation authorization boundary established by ADR-0012. Deployment and parent-checkout module layout are documented in ../../../FGF-IDP-INTEGRATION.md.
