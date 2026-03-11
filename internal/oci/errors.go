package oci

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// OCI Distribution Spec error codes.
const (
	CodeNameUnknown     = "NAME_UNKNOWN"
	CodeManifestUnknown = "MANIFEST_UNKNOWN"
	CodeBlobUnknown     = "BLOB_UNKNOWN"
)

// OCIError represents a single OCI Distribution Spec error entry.
type OCIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// OCIErrors represents an OCI Distribution Spec error response body.
type OCIErrors struct {
	Errors []OCIError `json:"errors"`
}

// writeOCIError writes an OCI-compliant error response with the given status
// code, error code, and message.
func writeOCIError(c *gin.Context, status int, code, message string) {
	c.JSON(status, OCIErrors{
		Errors: []OCIError{
			{Code: code, Message: message},
		},
	})
}

// writeNameUnknown writes a 404 NAME_UNKNOWN error response.
func writeNameUnknown(c *gin.Context, repo string) {
	writeOCIError(c, http.StatusNotFound, CodeNameUnknown, fmt.Sprintf("repository %q not known to registry", repo))
}

// writeManifestUnknown writes a 404 MANIFEST_UNKNOWN error response.
func writeManifestUnknown(c *gin.Context) {
	writeOCIError(c, http.StatusNotFound, CodeManifestUnknown, "manifest unknown to registry")
}

// writeBlobUnknown writes a 404 BLOB_UNKNOWN error response.
func writeBlobUnknown(c *gin.Context) {
	writeOCIError(c, http.StatusNotFound, CodeBlobUnknown, "blob unknown to registry")
}
