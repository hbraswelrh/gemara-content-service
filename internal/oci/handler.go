package oci

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Handler provides HTTP handler methods for OCI Distribution Spec v2
// read-only endpoints.
type Handler struct {
	registry *Registry
}

// NewHandler creates a Handler backed by the given Registry.
func NewHandler(registry *Registry) *Handler {
	return &Handler{registry: registry}
}

// V2Check handles GET /v2/ — the OCI handshake endpoint (FR-001).
func (h *Handler) V2Check(c *gin.Context) {
	c.Header("Docker-Distribution-API-Version", "registry/2.0")
	c.JSON(http.StatusOK, gin.H{})
}

// ListCatalog handles GET /v2/_catalog — list all repositories (FR-002).
func (h *Handler) ListCatalog(c *gin.Context) {
	repos, err := h.registry.ListRepositories()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"repositories": repos})
}

// ListTags handles GET /v2/{name}/tags/list — list tags for a repository (FR-003).
func (h *Handler) ListTags(c *gin.Context, name string) {
	tags, err := h.registry.ListTags(name)
	if err != nil {
		if errors.Is(err, ErrRepoNotFound) {
			writeNameUnknown(c, name)
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"name": name,
		"tags": tags,
	})
}

// GetManifest handles GET/HEAD /v2/{name}/manifests/{reference} — get an OCI
// manifest by tag or digest (FR-004, FR-010).
func (h *Handler) GetManifest(c *gin.Context, name, reference string) {
	data, digest, err := h.registry.GetManifest(name, reference)
	if err != nil {
		if errors.Is(err, ErrManifestNotFound) {
			writeManifestUnknown(c)
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	c.Header("Docker-Content-Digest", digest)
	c.Header("Content-Length", fmt.Sprintf("%d", len(data)))
	c.Header("Docker-Distribution-API-Version", "registry/2.0")

	if c.Request.Method == http.MethodHead {
		c.Status(http.StatusOK)
		return
	}
	c.Data(http.StatusOK, "application/vnd.oci.image.manifest.v1+json", data)
}

// GetBlob handles GET /v2/{name}/blobs/{digest} — stream a blob (FR-005, FR-011).
func (h *Handler) GetBlob(c *gin.Context, digest string) {
	blobPath, err := h.registry.BlobPath(digest)
	if err != nil {
		if errors.Is(err, ErrBlobNotFound) {
			writeBlobUnknown(c)
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Docker-Content-Digest", digest)
	c.File(blobPath)
}
