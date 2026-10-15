// @module OAuthAssuranceTests rejects cryptographically valid but unauthorized JWTs (A05).
package accessauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"testing"
	"time"
)

func TestA05JWTAdversarialClaims(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	forged, _ := rsa.GenerateKey(rand.Reader, 2048)
	issuer := "https://identity.example.invalid"
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != issuer+"/cdn-cgi/access/certs" {
			t.Fatal("unexpected egress")
		}
		body, _ := json.Marshal(map[string]any{"keys": []map[string]string{{"kty": "RSA", "kid": "test", "use": "sig", "alg": "RS256", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: ioBody(string(body)), Request: r}, nil
	})}
	verifier, err := NewJWTVerifier(issuer, "primary", client)
	if err != nil {
		t.Fatal(err)
	}
	for _, attack := range []string{"valid", "expired", "issuer", "audience", "subject", "signature"} {
		t.Run(attack, func(t *testing.T) {
			claims := map[string]any{"iss": issuer, "aud": []string{"primary"}, "sub": "synthetic-owner", "email": "owner@example.com", "exp": time.Now().Add(time.Hour).Unix()}
			signer := key
			switch attack {
			case "expired":
				claims["exp"] = time.Now().Add(-time.Hour).Unix()
			case "issuer":
				claims["iss"] = "https://attacker.example.invalid"
			case "audience":
				claims["aud"] = []string{"invitation"}
			case "subject":
				claims["sub"] = ""
			case "signature":
				signer = forged
			}
			_, err := verifier.Verify(context.Background(), signedToken(t, signer, claims))
			if (attack == "valid") != (err == nil) {
				t.Fatalf("claim oracle failed: %s %v", attack, err)
			}
		})
	}
}
