package oci

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	bolt "go.etcd.io/bbolt"
)

// newTestRegistry creates a temporary BBolt database and blob directory,
// populates them with the given tags, manifests, and blobs, and returns a
// ready-to-use Registry. The caller does not need to clean up; t.TempDir()
// handles it.
func newTestRegistry(t *testing.T, tags map[string]string, manifests map[string][]byte, blobs map[string][]byte) *Registry {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "index.db")
	blobRoot := filepath.Join(dir, "blobs")

	db, err := bolt.Open(dbPath, 0600, nil)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	err = db.Update(func(tx *bolt.Tx) error {
		if len(tags) > 0 {
			b, err := tx.CreateBucketIfNotExists(bucketTags)
			if err != nil {
				return err
			}
			for k, v := range tags {
				if err := b.Put([]byte(k), []byte(v)); err != nil {
					return err
				}
			}
		}
		if len(manifests) > 0 {
			b, err := tx.CreateBucketIfNotExists(bucketManifests)
			if err != nil {
				return err
			}
			for k, v := range manifests {
				if err := b.Put([]byte(k), v); err != nil {
					return err
				}
			}
		}
		return nil
	})
	require.NoError(t, err)

	for digest, content := range blobs {
		algo, hex, ok := parseDigest(digest)
		require.True(t, ok, "invalid test blob digest: %s", digest)
		prefix := hex[:2]
		blobDir := filepath.Join(blobRoot, algo, prefix)
		require.NoError(t, os.MkdirAll(blobDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(blobDir, hex), content, 0600))
	}

	return NewRegistry(db, blobRoot)
}

func TestListRepositories_Empty(t *testing.T) {
	reg := newTestRegistry(t, nil, nil, nil)
	repos, err := reg.ListRepositories()
	require.NoError(t, err)
	assert.Empty(t, repos)
}

func TestListRepositories(t *testing.T) {
	tags := map[string]string{
		"policies/rhel11:v1.0.0": "sha256:aaa",
		"policies/rhel11:v1.1.0": "sha256:bbb",
		"catalogs/osps-b:v1.0.0": "sha256:ccc",
		"guidance/nist:v1.0.0":   "sha256:ddd",
	}
	reg := newTestRegistry(t, tags, nil, nil)
	repos, err := reg.ListRepositories()
	require.NoError(t, err)
	assert.Len(t, repos, 3)
	assert.Contains(t, repos, "policies/rhel11")
	assert.Contains(t, repos, "catalogs/osps-b")
	assert.Contains(t, repos, "guidance/nist")
}

func TestRegistry_ListTags(t *testing.T) {
	tags := map[string]string{
		"policies/rhel11:v1.0.0": "sha256:aaa",
		"policies/rhel11:v1.1.0": "sha256:bbb",
		"catalogs/osps-b:v1.0.0": "sha256:ccc",
	}
	reg := newTestRegistry(t, tags, nil, nil)

	result, err := reg.ListTags("policies/rhel11")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"v1.0.0", "v1.1.0"}, result)
}

func TestListTags_RepoNotFound(t *testing.T) {
	tags := map[string]string{
		"policies/rhel11:v1.0.0": "sha256:aaa",
	}
	reg := newTestRegistry(t, tags, nil, nil)

	_, err := reg.ListTags("policies/nonexistent")
	assert.ErrorIs(t, err, ErrRepoNotFound)
}

func TestListTags_EmptyDB(t *testing.T) {
	reg := newTestRegistry(t, nil, nil, nil)

	_, err := reg.ListTags("policies/rhel11")
	assert.ErrorIs(t, err, ErrRepoNotFound)
}

func TestRegistry_GetManifest_ByTag(t *testing.T) {
	manifestJSON := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json"}`)
	tags := map[string]string{
		"catalogs/osps-b:v1.0.0": "sha256:abc123",
	}
	manifests := map[string][]byte{
		"sha256:abc123": manifestJSON,
	}
	reg := newTestRegistry(t, tags, manifests, nil)

	data, digest, err := reg.GetManifest("catalogs/osps-b", "v1.0.0")
	require.NoError(t, err)
	assert.Equal(t, manifestJSON, data)
	assert.Equal(t, "sha256:abc123", digest)
}

func TestRegistry_GetManifest_ByDigest(t *testing.T) {
	manifestJSON := []byte(`{"schemaVersion":2}`)
	manifests := map[string][]byte{
		"sha256:abc123": manifestJSON,
	}
	reg := newTestRegistry(t, nil, manifests, nil)

	data, digest, err := reg.GetManifest("catalogs/osps-b", "sha256:abc123")
	require.NoError(t, err)
	assert.Equal(t, manifestJSON, data)
	assert.Equal(t, "sha256:abc123", digest)
}

func TestGetManifest_TagNotFound(t *testing.T) {
	tags := map[string]string{
		"catalogs/osps-b:v1.0.0": "sha256:abc123",
	}
	reg := newTestRegistry(t, tags, nil, nil)

	_, _, err := reg.GetManifest("catalogs/osps-b", "v99.0.0")
	assert.ErrorIs(t, err, ErrManifestNotFound)
}

func TestGetManifest_DigestNotFound(t *testing.T) {
	reg := newTestRegistry(t, nil, nil, nil)

	_, _, err := reg.GetManifest("catalogs/osps-b", "sha256:nonexistent")
	assert.ErrorIs(t, err, ErrManifestNotFound)
}

func TestGetManifest_RawBytesPreserved(t *testing.T) {
	// Verify that the exact bytes stored are returned, including
	// specific whitespace and key ordering.
	raw := []byte(`{"schemaVersion":2,  "mediaType":"application/vnd.oci.image.manifest.v1+json"  }`)
	manifests := map[string][]byte{
		"sha256:exact": raw,
	}
	reg := newTestRegistry(t, nil, manifests, nil)

	data, _, err := reg.GetManifest("any", "sha256:exact")
	require.NoError(t, err)
	assert.Equal(t, raw, data, "returned bytes must be identical to stored bytes")
}

func TestBlobPath_Found(t *testing.T) {
	content := []byte("hello blob")
	blobs := map[string][]byte{
		"sha256:abcdef1234567890": content,
	}
	reg := newTestRegistry(t, nil, nil, blobs)

	p, err := reg.BlobPath("sha256:abcdef1234567890")
	require.NoError(t, err)

	data, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, content, data)
}

func TestBlobPath_NotFound(t *testing.T) {
	reg := newTestRegistry(t, nil, nil, nil)

	_, err := reg.BlobPath("sha256:0000000000000000")
	assert.ErrorIs(t, err, ErrBlobNotFound)
}

func TestBlobPath_InvalidDigest(t *testing.T) {
	reg := newTestRegistry(t, nil, nil, nil)

	_, err := reg.BlobPath("invalid")
	assert.ErrorIs(t, err, ErrBlobNotFound)
}

func TestBlobPath_PathTraversal(t *testing.T) {
	reg := newTestRegistry(t, nil, nil, nil)

	_, err := reg.BlobPath("sha256:../../../../../../etc/passwd")
	assert.ErrorIs(t, err, ErrBlobNotFound)
}
