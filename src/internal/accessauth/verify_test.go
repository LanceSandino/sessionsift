package accessauth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestJWTVerifierValidatesAccessIdentity(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	issuer := "https://team.cloudflareaccess.com"
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != issuer+"/cdn-cgi/access/certs" {
			t.Fatalf("unexpected key URL: %s", request.URL)
		}
		n := base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes())
		e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.PublicKey.E)).Bytes())
		body, _ := json.Marshal(map[string]any{"keys": []map[string]string{{"kty": "RSA", "kid": "test", "use": "sig", "alg": "RS256", "n": n, "e": e}}})
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: ioBody(string(body)), Request: request}, nil
	})}
	verifier, err := NewJWTVerifier(issuer, "audience", client)
	if err != nil {
		t.Fatal(err)
	}
	token := signedToken(t, key, map[string]any{"iss": issuer, "aud": []string{"audience"}, "sub": "subject", "email": "Owner@Example.com", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Add(-time.Minute).Unix()})
	principal, err := verifier.Verify(context.Background(), token)
	if err != nil || principal.Email != "owner@example.com" || principal.Subject != "subject" {
		t.Fatalf("principal=%+v err=%v", principal, err)
	}
	bad := signedToken(t, key, map[string]any{"iss": issuer, "aud": []string{"wrong"}, "sub": "subject", "email": "owner@example.com", "exp": time.Now().Add(time.Hour).Unix()})
	if _, err = verifier.Verify(context.Background(), bad); err == nil {
		t.Fatal("wrong audience accepted")
	}
}

func TestJWTVerifierRequiresHTTPSConfiguration(t *testing.T) {
	for _, value := range []struct{ issuer, audience string }{{"http://team.example", "aud"}, {"https://team.example", ""}} {
		if _, err := NewJWTVerifier(value.issuer, value.audience, nil); err == nil {
			t.Fatalf("accepted issuer=%q audience=%q", value.issuer, value.audience)
		}
	}
}

func TestVerifierAdapterEnvironmentAndEmptyAssertion(t *testing.T) {
	t.Setenv("SESSIONSIFT_ACCESS_ISSUER", "https://team.cloudflareaccess.com")
	t.Setenv("SESSIONSIFT_ACCESS_AUD", "audience")
	verifier, err := NewFromEnv(nil)
	if err != nil || verifier == nil {
		t.Fatalf("environment verifier=%v err=%v", verifier, err)
	}
	if _, err = verifier.Verify(context.Background(), ""); err == nil {
		t.Fatal("empty assertion accepted")
	}
	var nilVerifier *JWTVerifier
	if _, err = nilVerifier.Verify(context.Background(), "token"); err == nil {
		t.Fatal("nil verifier accepted an assertion")
	}
	adapter := VerifierFunc(func(_ context.Context, assertion string) (Principal, error) {
		return Principal{Subject: assertion, Email: "owner@example.com"}, nil
	})
	principal, err := adapter.Verify(context.Background(), "subject")
	if err != nil || principal.Subject != "subject" {
		t.Fatalf("adapter principal=%+v err=%v", principal, err)
	}
}

func signedToken(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "test", "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	message := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(message))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return message + "." + base64.RawURLEncoding.EncodeToString(signature)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func ioBody(value string) *bodyReader { return &bodyReader{Reader: strings.NewReader(value)} }

type bodyReader struct{ *strings.Reader }

func (body *bodyReader) Close() error { return nil }
