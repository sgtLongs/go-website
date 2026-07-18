package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestNormalizeBasePath(t *testing.T) {
	t.Parallel()

	valid := map[string]string{
		"":                "",
		"/":               "",
		"/beta":           "/beta",
		"/beta/":          "/beta",
		" /beta/preview ": "/beta/preview",
	}
	for input, want := range valid {
		input, want := input, want
		t.Run("valid_"+input, func(t *testing.T) {
			t.Parallel()
			got, err := normalizeBasePath(input)
			if err != nil {
				t.Fatalf("normalizeBasePath(%q) returned error: %v", input, err)
			}
			if got != want {
				t.Fatalf("normalizeBasePath(%q) = %q, want %q", input, got, want)
			}
		})
	}

	invalid := []string{
		"beta",
		"//beta",
		"/beta//preview",
		"/beta/../prod",
		"/beta?preview=true",
		"/be ta",
		"/beta:*",
	}
	for _, input := range invalid {
		input := input
		t.Run("invalid_"+input, func(t *testing.T) {
			t.Parallel()
			if _, err := normalizeBasePath(input); err == nil {
				t.Fatalf("normalizeBasePath(%q) returned no error", input)
			}
		})
	}
}

func TestRootRoutesRemainUnprefixed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := newRouter()

	response := request(t, router, http.MethodGet, "/", nil)
	assertStatus(t, response, http.StatusOK)
	assertBodyContains(t, response, `<base href="/">`)

	response = request(t, router, http.MethodGet, "/assets/css/lobbies.css", nil)
	assertStatus(t, response, http.StatusOK)

	response = request(t, router, http.MethodGet, "/health", nil)
	assertStatus(t, response, http.StatusOK)

	lobbyID, accessCookie := createLobby(t, router, "/api/lobbies")
	if accessCookie.Path != "/" {
		t.Fatalf("root access cookie Path = %q, want /", accessCookie.Path)
	}
	if want := "lobby_access_" + lobbyID; accessCookie.Name != want {
		t.Fatalf("root access cookie name = %q, want %q", accessCookie.Name, want)
	}

	response = request(t, router, http.MethodGet, "/room/"+lobbyID, nil, accessCookie)
	assertStatus(t, response, http.StatusOK)
	assertBodyContains(t, response, `<base href="/">`)

	response = request(t, router, http.MethodPost, "/api/lobbies/"+lobbyID+"/tab-session", nil, accessCookie)
	assertStatus(t, response, http.StatusCreated)
	var tabSession struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &tabSession); err != nil || tabSession.Token == "" {
		t.Fatalf("tab session response = %q, decode error = %v", response.Body.String(), err)
	}
}

func TestBetaRoutesUseBasePath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := newRouterWithBasePath("/beta")

	response := request(t, router, http.MethodGet, "/beta", nil)
	assertStatus(t, response, http.StatusPermanentRedirect)
	if location := response.Header().Get("Location"); location != "/beta/" {
		t.Fatalf("canonical redirect Location = %q, want /beta/", location)
	}

	response = request(t, router, http.MethodGet, "/beta/", nil)
	assertStatus(t, response, http.StatusOK)
	assertBodyContains(t, response, `<base href="/beta/">`)
	assertBodyContains(t, response, `href="assets/css/lobbies.css"`)

	for _, path := range []string{
		"/beta/assets/css/lobbies.css",
		"/beta/assets/js/lobbies.js",
		"/beta/api/lobbies",
		"/beta/health",
	} {
		response = request(t, router, http.MethodGet, path, nil)
		assertStatus(t, response, http.StatusOK)
	}

	for _, path := range []string{
		"/",
		"/assets/css/lobbies.css",
		"/api/lobbies",
		"/health",
		"/ws/rooms/00000000-0000-0000-0000-000000000000",
	} {
		response = request(t, router, http.MethodGet, path, nil)
		assertStatus(t, response, http.StatusNotFound)
	}

	lobbyID, accessCookie := createLobby(t, router, "/beta/api/lobbies")
	if accessCookie.Path != "/beta/" {
		t.Fatalf("beta access cookie Path = %q, want /beta/", accessCookie.Path)
	}
	if want := "beta_lobby_access_" + lobbyID; accessCookie.Name != want {
		t.Fatalf("beta access cookie name = %q, want %q", accessCookie.Name, want)
	}

	response = request(t, router, http.MethodGet, "/beta/room/"+lobbyID, nil)
	assertStatus(t, response, http.StatusSeeOther)
	if location := response.Header().Get("Location"); location != "/beta/" {
		t.Fatalf("unauthorized room redirect Location = %q, want /beta/", location)
	}

	response = request(t, router, http.MethodGet, "/beta/room/"+lobbyID, nil, accessCookie)
	assertStatus(t, response, http.StatusOK)
	assertBodyContains(t, response, `<base href="/beta/">`)
	assertBodyContains(t, response, `src="assets/js/room.js"`)

	joinBody := strings.NewReader(`{"password":"secret"}`)
	response = request(t, router, http.MethodPost, "/beta/api/lobbies/"+lobbyID+"/join", joinBody)
	assertStatus(t, response, http.StatusNoContent)
	joinedCookies := response.Result().Cookies()
	if len(joinedCookies) != 1 || joinedCookies[0].Path != "/beta/" || !strings.HasPrefix(joinedCookies[0].Name, "beta_lobby_access_") {
		t.Fatalf("join cookie = %#v, want one beta-scoped access cookie", joinedCookies)
	}

	response = request(t, router, http.MethodGet, "/beta/ws/rooms/"+lobbyID, nil, accessCookie)
	assertStatus(t, response, http.StatusBadRequest)
}

func createLobby(t *testing.T, router http.Handler, path string) (string, *http.Cookie) {
	t.Helper()

	body := strings.NewReader(`{"name":"Test lobby","password":"secret"}`)
	response := request(t, router, http.MethodPost, path, body)
	assertStatus(t, response, http.StatusCreated)

	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.ID == "" {
		t.Fatal("create response has empty lobby ID")
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("create response cookies = %#v, want one", cookies)
	}
	return created.ID, cookies[0]
}

func request(t *testing.T, router http.Handler, method, target string, body io.Reader, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()

	httpRequest := httptest.NewRequest(method, target, body)
	if body != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	for _, cookie := range cookies {
		httpRequest.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httpRequest)
	return response
}

func assertStatus(t *testing.T, response *httptest.ResponseRecorder, want int) {
	t.Helper()
	if response.Code != want {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, want, response.Body.String())
	}
}

func assertBodyContains(t *testing.T, response *httptest.ResponseRecorder, want string) {
	t.Helper()
	if !strings.Contains(response.Body.String(), want) {
		t.Fatalf("response body does not contain %q", want)
	}
}
