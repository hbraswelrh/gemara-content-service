package oci

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// setupRouter creates a Gin engine with OCI routes backed by the given
// registry, suitable for httptest.
func setupRouter(reg *Registry) *gin.Engine {
	r := gin.New()
	RegisterRoutes(r, reg)
	return r
}

// --- V2 Check (handshake) ---

func TestV2Check(t *testing.T) {
	reg := newTestRegistry(t, nil, nil, nil)
	router := setupRouter(reg)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/v2/", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "registry/2.0", w.Header().Get("Docker-Distribution-API-Version"))

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Empty(t, body)
}

// --- Catalog ---

func TestListCatalog_Empty(t *testing.T) {
	reg := newTestRegistry(t, nil, nil, nil)
	router := setupRouter(reg)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/v2/_catalog", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Repositories []string `json:"repositories"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Empty(t, body.Repositories)
}

func TestListCatalog_WithRepos(t *testing.T) {
	tags := map[string]string{
		"policies/rhel11:v1.0.0": "sha256:aaa",
		"catalogs/osps-b:v1.0.0": "sha256:bbb",
	}
	reg := newTestRegistry(t, tags, nil, nil)
	router := setupRouter(reg)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/v2/_catalog", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Repositories []string `json:"repositories"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Len(t, body.Repositories, 2)
	assert.Contains(t, body.Repositories, "policies/rhel11")
	assert.Contains(t, body.Repositories, "catalogs/osps-b")
}

// --- Tags ---

func TestListTags(t *testing.T) {
	tags := map[string]string{
		"policies/rhel11:v1.0.0": "sha256:aaa",
		"policies/rhel11:v1.1.0": "sha256:bbb",
	}
	reg := newTestRegistry(t, tags, nil, nil)
	router := setupRouter(reg)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/v2/policies/rhel11/tags/list", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "policies/rhel11", body.Name)
	assert.ElementsMatch(t, []string{"v1.0.0", "v1.1.0"}, body.Tags)
}

func TestListTags_NotFound(t *testing.T) {
	reg := newTestRegistry(t, nil, nil, nil)
	router := setupRouter(reg)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/v2/policies/nonexistent/tags/list", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var body OCIErrors
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Errors, 1)
	assert.Equal(t, CodeNameUnknown, body.Errors[0].Code)
}

// --- Manifests ---

func TestGetManifest_ByTag(t *testing.T) {
	manifestJSON := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json"}`)
	tags := map[string]string{
		"catalogs/osps-b:v1.0.0": "sha256:abc123",
	}
	manifests := map[string][]byte{
		"sha256:abc123": manifestJSON,
	}
	reg := newTestRegistry(t, tags, manifests, nil)
	router := setupRouter(reg)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/v2/catalogs/osps-b/manifests/v1.0.0", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/vnd.oci.image.manifest.v1+json", w.Header().Get("Content-Type"))
	assert.Equal(t, manifestJSON, w.Body.Bytes())
}

func TestGetManifest_ByDigest(t *testing.T) {
	manifestJSON := []byte(`{"schemaVersion":2}`)
	manifests := map[string][]byte{
		"sha256:abc123": manifestJSON,
	}
	reg := newTestRegistry(t, nil, manifests, nil)
	router := setupRouter(reg)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/v2/catalogs/osps-b/manifests/sha256:abc123", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, manifestJSON, w.Body.Bytes())
}

func TestGetManifest_DockerContentDigest(t *testing.T) {
	manifestJSON := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json"}`)
	tags := map[string]string{
		"catalogs/osps-b:v1.0.0": "sha256:abc123",
	}
	manifests := map[string][]byte{
		"sha256:abc123": manifestJSON,
	}
	reg := newTestRegistry(t, tags, manifests, nil)
	router := setupRouter(reg)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/v2/catalogs/osps-b/manifests/v1.0.0", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "sha256:abc123", w.Header().Get("Docker-Content-Digest"))
	assert.Equal(t, "registry/2.0", w.Header().Get("Docker-Distribution-API-Version"))
}

func TestHeadManifest_ByTag(t *testing.T) {
	manifestJSON := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json"}`)
	tags := map[string]string{
		"catalogs/osps-b:v1.0.0": "sha256:abc123",
	}
	manifests := map[string][]byte{
		"sha256:abc123": manifestJSON,
	}
	reg := newTestRegistry(t, tags, manifests, nil)
	router := setupRouter(reg)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodHead, "/v2/catalogs/osps-b/manifests/v1.0.0", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "sha256:abc123", w.Header().Get("Docker-Content-Digest"))
	assert.Equal(t, "application/vnd.oci.image.manifest.v1+json", w.Header().Get("Content-Type"))
	assert.Empty(t, w.Body.Bytes(), "HEAD should return no body")
}

func TestHeadManifest_NotFound(t *testing.T) {
	reg := newTestRegistry(t, nil, nil, nil)
	router := setupRouter(reg)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodHead, "/v2/policies/rhel11/manifests/v99.0.0", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGetManifest_NotFound(t *testing.T) {
	reg := newTestRegistry(t, nil, nil, nil)
	router := setupRouter(reg)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/v2/policies/rhel11/manifests/v99.0.0", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var body OCIErrors
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Errors, 1)
	assert.Equal(t, CodeManifestUnknown, body.Errors[0].Code)
}

// --- Blobs ---

func TestGetBlob(t *testing.T) {
	blobContent := []byte("policy: deny-root-user\n")
	blobs := map[string][]byte{
		"sha256:abcdef1234567890": blobContent,
	}
	reg := newTestRegistry(t, nil, nil, blobs)
	router := setupRouter(reg)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/v2/policies/rhel11/blobs/sha256:abcdef1234567890", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/octet-stream", w.Header().Get("Content-Type"))

	body, err := io.ReadAll(w.Body)
	require.NoError(t, err)
	assert.Equal(t, blobContent, body)
}

func TestGetBlob_DockerContentDigest(t *testing.T) {
	blobContent := []byte("policy: deny-root-user\n")
	blobs := map[string][]byte{
		"sha256:abcdef1234567890": blobContent,
	}
	reg := newTestRegistry(t, nil, nil, blobs)
	router := setupRouter(reg)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/v2/policies/rhel11/blobs/sha256:abcdef1234567890", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "sha256:abcdef1234567890", w.Header().Get("Docker-Content-Digest"))
}

func TestGetBlob_NotFound(t *testing.T) {
	reg := newTestRegistry(t, nil, nil, nil)
	router := setupRouter(reg)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/v2/policies/rhel11/blobs/sha256:0000000000000000", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var body OCIErrors
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Errors, 1)
	assert.Equal(t, CodeBlobUnknown, body.Errors[0].Code)
}

// --- Path dispatch edge cases ---

func TestDispatch_UnknownPath(t *testing.T) {
	reg := newTestRegistry(t, nil, nil, nil)
	router := setupRouter(reg)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/v2/some/random/path", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestDispatch_DeepRepoName(t *testing.T) {
	tags := map[string]string{
		"org/team/policies/rhel11:v1.0.0": "sha256:aaa",
	}
	reg := newTestRegistry(t, tags, nil, nil)
	router := setupRouter(reg)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/v2/org/team/policies/rhel11/tags/list", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "org/team/policies/rhel11", body.Name)
	assert.Equal(t, []string{"v1.0.0"}, body.Tags)
}
