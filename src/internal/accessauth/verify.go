// Package accessauth verifies Cloudflare Access identity assertions at the origin.
// @module AccessAuth keeps edge authentication separate from local capability authorization.
package accessauth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Principal is the verified identity forwarded by Cloudflare Access.
type Principal struct {
	Subject string `json:"subject"`
	Email   string `json:"email"`
}

// Verifier validates the signed Cf-Access-Jwt-Assertion value.
type Verifier interface {
	Verify(context.Context, string) (Principal, error)
}

// VerifierFunc adapts a function for tests and deployment-specific verification.
type VerifierFunc func(context.Context, string) (Principal, error)

func (function VerifierFunc) Verify(ctx context.Context, assertion string) (Principal, error) {
	return function(ctx, assertion)
}

// JWTVerifier validates Access JWT signatures, issuer, audience, expiry, and identity claims.
type JWTVerifier struct{ verifier *oidc.IDTokenVerifier }

// NewJWTVerifier constructs a verifier without contacting Access until the first assertion is checked.
func NewJWTVerifier(issuer, audience string, client *http.Client) (*JWTVerifier, error) {
	issuer = strings.TrimRight(strings.TrimSpace(issuer), "/")
	audience = strings.TrimSpace(audience)
	parsed, err := url.Parse(issuer)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" || audience == "" {
		return nil, errors.New("Access issuer must be an HTTPS origin and audience is required")
	}
	ctx := context.Background()
	if client != nil {
		ctx = oidc.ClientContext(ctx, client)
	}
	keys := oidc.NewRemoteKeySet(ctx, issuer+"/cdn-cgi/access/certs")
	return &JWTVerifier{verifier: oidc.NewVerifier(issuer, keys, &oidc.Config{ClientID: audience})}, nil
}

// NewFromEnv reads the explicit Cloudflare Access origin-verification configuration.
func NewFromEnv(client *http.Client) (*JWTVerifier, error) {
	return NewJWTVerifier(os.Getenv("SESSIONSIFT_ACCESS_ISSUER"), os.Getenv("SESSIONSIFT_ACCESS_AUD"), client)
}

// Verify validates an assertion and returns only the identity used for local authorization.
func (verifier *JWTVerifier) Verify(ctx context.Context, assertion string) (Principal, error) {
	if verifier == nil || verifier.verifier == nil || strings.TrimSpace(assertion) == "" {
		return Principal{}, errors.New("Access assertion is required")
	}
	token, err := verifier.verifier.Verify(ctx, assertion)
	if err != nil {
		return Principal{}, errors.New("Access assertion is invalid")
	}
	var claims struct {
		Subject string `json:"sub"`
		Email   string `json:"email"`
	}
	if err = token.Claims(&claims); err != nil {
		return Principal{}, errors.New("Access identity claims are invalid")
	}
	claims.Email = strings.ToLower(strings.TrimSpace(claims.Email))
	if claims.Subject == "" || claims.Email == "" || !strings.Contains(claims.Email, "@") {
		return Principal{}, errors.New("Access identity claims are incomplete")
	}
	return Principal{Subject: claims.Subject, Email: claims.Email}, nil
}
