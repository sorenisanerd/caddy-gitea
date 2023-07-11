package gitea

import (
	"net/http"
	"testing"

	"gotest.tools/assert"
)

func TestMiddleware_inferOwnerRepoPathAndRef(t *testing.T) {
	tests := []struct {
		name         string
		domain       string
		url          string
		wantOwner    string
		wantRepo     string
		wantFilePath string
		wantRef      string
	}{
		{"gitea-pages repo", "example.com", "http://someorg.example.com/a/b/c", "someorg", "", "/a/b/c", ""},
		{"other repo", "example.com", "http://somerepo.someorg.example.com/a/b/c", "someorg", "somerepo", "/a/b/c", ""},
		{"other branch", "example.com", "http://somebranch.somerepo.someorg.example.com/a/b/c", "someorg", "somerepo", "/a/b/c", "somebranch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := Middleware{
				Domain: tt.domain,
			}
			req, _ := http.NewRequest("GET", tt.url, nil)
			gotOwner, gotRepo, gotFilePath, gotRef := m.inferOwnerRepoPathAndRef(req)
			assert.Equal(t, tt.wantOwner, gotOwner)
			assert.Equal(t, tt.wantRepo, gotRepo)
			assert.Equal(t, tt.wantFilePath, gotFilePath)
			assert.Equal(t, tt.wantRef, gotRef)
		})
	}
}
