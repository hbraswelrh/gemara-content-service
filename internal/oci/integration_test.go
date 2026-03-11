package oci

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOCIWorkflow_EndToEnd exercises the complete OCI pull flow:
// handshake → catalog → tags → manifest → blob download.
// It uses a real HTTP test server to verify the full stack.
func TestOCIWorkflow_EndToEnd(t *testing.T) {
	// Build a realistic manifest and blob for catalogs/osps-b:v1.0.0.
	catalogYAML := []byte("id: OSPS-B\nname: Open Source Project Security Baseline\n")
	catalogDigest := sha256Digest(catalogYAML)

	emptyConfig := []byte("{}")
	emptyConfigDigest := sha256Digest(emptyConfig)

	manifestObj := map[string]interface{}{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"config": map[string]interface{}{
			"mediaType": "application/vnd.oci.empty.v1+json",
			"digest":    emptyConfigDigest,
			"size":      len(emptyConfig),
		},
		"layers": []map[string]interface{}{
			{
				"mediaType": "application/vnd.gemara.catalog.v1+yaml",
				"digest":    catalogDigest,
				"size":      len(catalogYAML),
			},
		},
	}
	manifestBytes, err := json.Marshal(manifestObj)
	require.NoError(t, err)
	manifestDigest := sha256Digest(manifestBytes)

	tags := map[string]string{
		"catalogs/osps-b:v1.0.0": manifestDigest,
		"policies/rhel11:v1.0.0": manifestDigest,
	}
	manifests := map[string][]byte{
		manifestDigest: manifestBytes,
	}
	blobs := map[string][]byte{
		catalogDigest:     catalogYAML,
		emptyConfigDigest: emptyConfig,
	}

	reg := newTestRegistry(t, tags, manifests, blobs)
	router := setupRouter(reg)
	srv := httptest.NewServer(router)
	defer srv.Close()

	client := srv.Client()

	// 1. Handshake: GET /v2/
	t.Run("handshake", func(t *testing.T) {
		resp, err := client.Get(srv.URL + "/v2/")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "registry/2.0", resp.Header.Get("Docker-Distribution-API-Version"))
	})

	// 2. Catalog: GET /v2/_catalog
	t.Run("catalog", func(t *testing.T) {
		resp, err := client.Get(srv.URL + "/v2/_catalog")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var body struct {
			Repositories []string `json:"repositories"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		assert.Contains(t, body.Repositories, "catalogs/osps-b")
		assert.Contains(t, body.Repositories, "policies/rhel11")
	})

	// 3. Tags: GET /v2/catalogs/osps-b/tags/list
	t.Run("tags", func(t *testing.T) {
		resp, err := client.Get(srv.URL + "/v2/catalogs/osps-b/tags/list")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var body struct {
			Name string   `json:"name"`
			Tags []string `json:"tags"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		assert.Equal(t, "catalogs/osps-b", body.Name)
		assert.Equal(t, []string{"v1.0.0"}, body.Tags)
	})

	// 4. Manifest by tag: GET /v2/catalogs/osps-b/manifests/v1.0.0
	var layerDigest string
	t.Run("manifest_by_tag", func(t *testing.T) {
		resp, err := client.Get(srv.URL + "/v2/catalogs/osps-b/manifests/v1.0.0")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/vnd.oci.image.manifest.v1+json", resp.Header.Get("Content-Type"))

		var m struct {
			SchemaVersion int `json:"schemaVersion"`
			Layers        []struct {
				Digest    string `json:"digest"`
				MediaType string `json:"mediaType"`
			} `json:"layers"`
		}
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&m))
		assert.Equal(t, 2, m.SchemaVersion)
		require.Len(t, m.Layers, 1)
		assert.Equal(t, "application/vnd.gemara.catalog.v1+yaml", m.Layers[0].MediaType)
		layerDigest = m.Layers[0].Digest
	})

	// 5. Manifest by digest: GET /v2/catalogs/osps-b/manifests/sha256:...
	t.Run("manifest_by_digest", func(t *testing.T) {
		resp, err := client.Get(srv.URL + "/v2/catalogs/osps-b/manifests/" + manifestDigest)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, manifestBytes, readAll(t, resp.Body))
	})

	// 6. Blob download: GET /v2/catalogs/osps-b/blobs/{digest}
	t.Run("blob_download", func(t *testing.T) {
		require.NotEmpty(t, layerDigest, "layer digest should have been extracted from manifest")

		resp, err := client.Get(srv.URL + "/v2/catalogs/osps-b/blobs/" + layerDigest)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/octet-stream", resp.Header.Get("Content-Type"))
		assert.Equal(t, catalogYAML, readAll(t, resp.Body))
	})

	// 7. Error cases
	t.Run("404_unknown_repo", func(t *testing.T) {
		resp, err := client.Get(srv.URL + "/v2/nonexistent/repo/tags/list")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		var body OCIErrors
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		assert.Equal(t, CodeNameUnknown, body.Errors[0].Code)
	})

	t.Run("404_unknown_tag", func(t *testing.T) {
		resp, err := client.Get(srv.URL + "/v2/catalogs/osps-b/manifests/v99.0.0")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		var body OCIErrors
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		assert.Equal(t, CodeManifestUnknown, body.Errors[0].Code)
	})

	t.Run("404_unknown_blob", func(t *testing.T) {
		resp, err := client.Get(srv.URL + "/v2/catalogs/osps-b/blobs/sha256:0000000000000000000000000000000000000000000000000000000000000000")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		var body OCIErrors
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		assert.Equal(t, CodeBlobUnknown, body.Errors[0].Code)
	})
}

func sha256Digest(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", h)
}

func readAll(t *testing.T, r io.Reader) []byte {
	t.Helper()
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	return data
}
