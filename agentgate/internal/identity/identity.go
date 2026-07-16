// Package identity resolves WHO is making a request from the ext_authz
// check request.
//
// Trust model: the gateway (agentgateway) validates the JWT's signature,
// expiry, and issuer BEFORE this service ever sees the request (jwtAuth
// policy, mode: strict). So by the time we run, the claims are trustworthy —
// we only need to read them, not re-verify them.
package identity

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	auth_pb "github.com/envoyproxy/go-control-plane/envoy/service/auth/v3"
)

// Identity is the resolved "who" of a request.
type Identity struct {
	// AgentID identifies the software agent (the OAuth client id, e.g.
	// "agent-client").
	AgentID string
	// OnBehalfOf is the human the agent is acting for (from the custom
	// on_behalf_of claim; falls back to preferred_username).
	OnBehalfOf string
	// Roles are the realm roles Keycloak granted that human.
	Roles []string
	// Subject is the raw JWT `sub` (Keycloak's internal user UUID).
	Subject string
}

// FromCheckRequest resolves the identity for a check request. It prefers the
// validated claims agentgateway forwards in metadata_context (populated by
// the gateway's jwtAuth policy); if absent, it falls back to decoding the
// Authorization header's JWT payload directly — safe because the gateway
// already verified the signature in strict mode.
func FromCheckRequest(req *auth_pb.CheckRequest) (*Identity, error) {
	if claims := claimsFromMetadata(req); claims != nil {
		return fromClaims(claims), nil
	}
	authHeader := req.GetAttributes().GetRequest().GetHttp().GetHeaders()["authorization"]
	if authHeader == "" {
		return nil, fmt.Errorf("no validated claims in metadata and no authorization header")
	}
	claims, err := decodeJWTPayload(authHeader)
	if err != nil {
		return nil, fmt.Errorf("decode bearer token: %w", err)
	}
	return fromClaims(claims), nil
}

// claimsFromMetadata digs the JWT claims out of the check request's
// metadata_context. agentgateway stores them under the Envoy-compatible
// "envoy.filters.http.jwt_authn" filter-metadata key; the exact nesting can
// vary, so we walk the structure looking for a map that has JWT-shaped keys.
func claimsFromMetadata(req *auth_pb.CheckRequest) map[string]any {
	md := req.GetAttributes().GetMetadataContext().GetFilterMetadata()
	for _, st := range md {
		if m := findClaimsMap(st.AsMap()); m != nil {
			return m
		}
	}
	return nil
}

// findClaimsMap recursively searches a decoded structpb tree for a map that
// looks like a JWT claim set (has "sub" or "on_behalf_of").
func findClaimsMap(v any) map[string]any {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	if _, has := m["sub"]; has {
		return m
	}
	if _, has := m["on_behalf_of"]; has {
		return m
	}
	for _, child := range m {
		if found := findClaimsMap(child); found != nil {
			return found
		}
	}
	return nil
}

// decodeJWTPayload extracts the claim set from "Bearer <jwt>" WITHOUT
// verifying the signature. Only sound because the gateway verified it first.
func decodeJWTPayload(authHeader string) (map[string]any, error) {
	token := strings.TrimPrefix(authHeader, "Bearer ")
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("not a JWT (want 3 segments, got %d)", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, err
	}
	return claims, nil
}

func fromClaims(claims map[string]any) *Identity {
	id := &Identity{
		AgentID:    str(claims["azp"]), // "authorized party" = the OAuth client
		OnBehalfOf: str(claims["on_behalf_of"]),
		Subject:    str(claims["sub"]),
	}
	if id.OnBehalfOf == "" {
		id.OnBehalfOf = str(claims["preferred_username"])
	}
	// Roles from our flat custom claim; fall back to Keycloak's default
	// realm_access.roles location.
	if roles, ok := claims["roles"].([]any); ok {
		for _, r := range roles {
			id.Roles = append(id.Roles, str(r))
		}
	} else if ra, ok := claims["realm_access"].(map[string]any); ok {
		if roles, ok := ra["roles"].([]any); ok {
			for _, r := range roles {
				id.Roles = append(id.Roles, str(r))
			}
		}
	}
	return id
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// HasRole reports whether the identity carries the given realm role.
func (id *Identity) HasRole(role string) bool {
	for _, r := range id.Roles {
		if r == role {
			return true
		}
	}
	return false
}
