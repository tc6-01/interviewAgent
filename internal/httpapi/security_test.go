package httpapi

import (
	"bytes"
	"net/http"
	"net/http/cookiejar"
	"testing"
	"time"
)

func TestAnonymousModeIssuesHardenedSubjectCookieAndIgnoresSpoofedHeader(t *testing.T) {
	server, _ := newInterviewTestServerWithSecurity(t, time.Minute, SecurityConfig{Mode: "anonymous"})
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := server.Client()
	client.Jar = jar

	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/documents/parse", bytes.NewBufferString(`{"kind":"jd","text":"Go backend"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Subject-ID", "attacker-controlled")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, readBody(response))
	}
	cookies := response.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies=%#v", cookies)
	}
	cookie := cookies[0]
	if cookie.Name != subjectCookieName || !validAnonymousSubject(cookie.Value) || cookie.Value == "attacker-controlled" || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" {
		t.Fatalf("cookie=%#v", cookie)
	}

	second, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/documents/parse", bytes.NewBufferString(`{"kind":"resume","text":"Go engineer"}`))
	second.Header.Set("Content-Type", "application/json")
	secondResponse, err := client.Do(second)
	if err != nil {
		t.Fatal(err)
	}
	defer secondResponse.Body.Close()
	if secondResponse.StatusCode != http.StatusOK || len(secondResponse.Cookies()) != 0 {
		t.Fatalf("second status=%d cookies=%#v", secondResponse.StatusCode, secondResponse.Cookies())
	}
}

func TestJWTModeRequiresBearerAndNeverFallsBackToCookie(t *testing.T) {
	server, _ := newInterviewTestServerWithSecurity(t, time.Minute, SecurityConfig{Mode: "jwt", JWTSecret: testJWTSecret})
	client := server.Client()

	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/documents/parse", bytes.NewBufferString(`{"kind":"jd","text":"Go backend"}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: subjectCookieName, Value: "anon_7d444840-9dc0-11d1-b245-5ffdce74fad2"})
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, response, http.StatusUnauthorized, "unauthenticated")

	authorized, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/documents/parse", bytes.NewBufferString(`{"kind":"jd","text":"Go backend"}`))
	authorized.Header.Set("Content-Type", "application/json")
	setBearer(t, authorized, "subject-a")
	authorizedResponse, err := client.Do(authorized)
	if err != nil {
		t.Fatal(err)
	}
	defer authorizedResponse.Body.Close()
	if authorizedResponse.StatusCode != http.StatusOK || len(authorizedResponse.Cookies()) != 0 {
		t.Fatalf("status=%d cookies=%#v", authorizedResponse.StatusCode, authorizedResponse.Cookies())
	}
}

func TestCrossOriginMatrixAllowsOnlyWhitelistedJWT(t *testing.T) {
	const allowedOrigin = "https://app.example.test"
	jwtServer, _ := newInterviewTestServerWithSecurity(t, time.Minute, SecurityConfig{
		Mode: "jwt", JWTSecret: testJWTSecret, AllowedOrigins: []string{allowedOrigin},
	})
	client := jwtServer.Client()

	preflight, _ := http.NewRequest(http.MethodOptions, jwtServer.URL+"/api/v1/interviews", nil)
	preflight.Header.Set("Origin", allowedOrigin)
	preflight.Header.Set("Access-Control-Request-Method", "POST")
	preflightResponse, err := client.Do(preflight)
	if err != nil {
		t.Fatal(err)
	}
	_ = preflightResponse.Body.Close()
	if preflightResponse.StatusCode != http.StatusNoContent || preflightResponse.Header.Get("Access-Control-Allow-Origin") != allowedOrigin || preflightResponse.Header.Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("preflight status=%d headers=%v", preflightResponse.StatusCode, preflightResponse.Header)
	}

	allowed, _ := http.NewRequest(http.MethodPost, jwtServer.URL+"/api/v1/documents/parse", bytes.NewBufferString(`{"kind":"jd","text":"Go backend"}`))
	allowed.Header.Set("Content-Type", "application/json")
	allowed.Header.Set("Origin", allowedOrigin)
	setBearer(t, allowed, "subject-a")
	allowedResponse, err := client.Do(allowed)
	if err != nil {
		t.Fatal(err)
	}
	defer allowedResponse.Body.Close()
	if allowedResponse.StatusCode != http.StatusOK || allowedResponse.Header.Get("Access-Control-Allow-Origin") != allowedOrigin {
		t.Fatalf("allowed status=%d headers=%v", allowedResponse.StatusCode, allowedResponse.Header)
	}

	denied, _ := http.NewRequest(http.MethodPost, jwtServer.URL+"/api/v1/documents/parse", bytes.NewBufferString(`{"kind":"jd","text":"Go backend"}`))
	denied.Header.Set("Content-Type", "application/json")
	denied.Header.Set("Origin", "https://evil.example.test")
	setBearer(t, denied, "subject-a")
	deniedResponse, err := client.Do(denied)
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, deniedResponse, http.StatusForbidden, "cors_origin_forbidden")

	anonymousServer, _ := newInterviewTestServerWithSecurity(t, time.Minute, SecurityConfig{Mode: "anonymous"})
	anonymous, _ := http.NewRequest(http.MethodPost, anonymousServer.URL+"/api/v1/documents/parse", bytes.NewBufferString(`{"kind":"jd","text":"Go backend"}`))
	anonymous.Header.Set("Content-Type", "application/json")
	anonymous.Header.Set("Origin", allowedOrigin)
	anonymousResponse, err := anonymousServer.Client().Do(anonymous)
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, anonymousResponse, http.StatusForbidden, "cross_origin_anonymous_forbidden")
}

func TestJWTSubjectCannotUseSpoofedHeaderToCrossBoundary(t *testing.T) {
	server, _ := newInterviewTestServer(t, time.Minute)
	client := server.Client()
	created := createInterviewHTTP(t, client, server.URL, "subject-a")

	request, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/interviews/"+created.InterviewID, nil)
	request.Header.Set("X-Subject-ID", "subject-a")
	setBearer(t, request, "subject-b")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, response, http.StatusNotFound, "interview_not_found")
}
