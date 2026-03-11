package oci

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes registers the OCI Distribution Spec v2 read-only endpoints
// on the given Gin router. Routes are registered under /v2/ using a single
// catch-all parameter to handle repository names that contain slashes.
func RegisterRoutes(router gin.IRouter, registry *Registry) {
	h := NewHandler(registry)
	d := dispatch(h)
	router.GET("/v2/*path", d)
	router.HEAD("/v2/*path", d)
}

// dispatch returns a Gin handler that parses the catch-all *path parameter
// and dispatches to the appropriate OCI handler method.
//
// Path patterns:
//   - /                          → V2Check (handshake)
//   - /_catalog                  → ListCatalog
//   - /{name...}/tags/list       → ListTags
//   - /{name...}/manifests/{ref} → GetManifest
//   - /{name...}/blobs/{digest}  → GetBlob
func dispatch(h *Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		p := c.Param("path")

		// Handshake: GET /v2/
		if p == "/" || p == "" {
			h.V2Check(c)
			return
		}

		// Catalog: GET /v2/_catalog
		if p == "/_catalog" {
			h.ListCatalog(c)
			return
		}

		// Tags list: GET /v2/{name}/tags/list
		if strings.HasSuffix(p, "/tags/list") {
			name := strings.TrimPrefix(p, "/")
			name = strings.TrimSuffix(name, "/tags/list")
			if name == "" {
				c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
				return
			}
			h.ListTags(c, name)
			return
		}

		// Manifests: GET /v2/{name}/manifests/{reference}
		if idx := strings.LastIndex(p, "/manifests/"); idx >= 0 {
			name := strings.TrimPrefix(p[:idx], "/")
			reference := p[idx+len("/manifests/"):]
			if name == "" || reference == "" {
				c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
				return
			}
			h.GetManifest(c, name, reference)
			return
		}

		// Blobs: GET /v2/{name}/blobs/{digest}
		if idx := strings.LastIndex(p, "/blobs/"); idx >= 0 {
			name := strings.TrimPrefix(p[:idx], "/")
			digest := p[idx+len("/blobs/"):]
			if name == "" || digest == "" {
				c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
				return
			}
			h.GetBlob(c, digest)
			return
		}

		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	}
}
