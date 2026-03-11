package oci

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	bolt "go.etcd.io/bbolt"
)

// Sentinel errors returned by Registry methods.
var (
	ErrRepoNotFound     = errors.New("repository not found")
	ErrManifestNotFound = errors.New("manifest not found")
	ErrBlobNotFound     = errors.New("blob not found")
)

// BBolt bucket names.
var (
	bucketTags      = []byte("tags")
	bucketManifests = []byte("manifests")
)

// Registry provides read-only access to OCI artifacts stored in a BBolt
// metadata index and a content-addressable filesystem blob store.
type Registry struct {
	db       *bolt.DB
	blobRoot string
}

// NewRegistry creates a Registry backed by the given BBolt database and
// filesystem blob root directory.
func NewRegistry(db *bolt.DB, blobRoot string) *Registry {
	return &Registry{
		db:       db,
		blobRoot: blobRoot,
	}
}

// ListRepositories returns all unique repository names derived from the
// tags stored in the index.
func (r *Registry) ListRepositories() ([]string, error) {
	seen := make(map[string]struct{})
	var repos []string

	err := r.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketTags)
		if b == nil {
			return nil
		}
		return b.ForEach(func(k, _ []byte) error {
			key := string(k)
			// Keys are formatted as {repo}:{tag}. Find the last colon
			// to split repo from tag (repo names may not contain colons,
			// but tags also should not, so last colon is the separator).
			idx := strings.LastIndex(key, ":")
			if idx < 0 {
				return nil
			}
			repo := key[:idx]
			if _, ok := seen[repo]; !ok {
				seen[repo] = struct{}{}
				repos = append(repos, repo)
			}
			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("listing repositories: %w", err)
	}

	if repos == nil {
		repos = []string{}
	}
	return repos, nil
}

// ListTags returns all tags for the given repository. If the repository has
// no tags (i.e. does not exist), ErrRepoNotFound is returned.
func (r *Registry) ListTags(repo string) ([]string, error) {
	prefix := repo + ":"
	var tags []string

	err := r.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketTags)
		if b == nil {
			return nil
		}
		c := b.Cursor()
		for k, _ := c.Seek([]byte(prefix)); k != nil && strings.HasPrefix(string(k), prefix); k, _ = c.Next() {
			tag := string(k)[len(prefix):]
			tags = append(tags, tag)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("listing tags for %s: %w", repo, err)
	}

	if tags == nil {
		return nil, ErrRepoNotFound
	}
	return tags, nil
}

// GetManifest returns the raw manifest JSON bytes and the manifest digest for
// the given repository and reference. The reference may be a tag or a digest
// (prefixed with "sha256:"). If the reference cannot be resolved,
// ErrManifestNotFound is returned. The returned bytes are the exact bytes
// stored in the index and MUST NOT be re-serialized.
func (r *Registry) GetManifest(repo, reference string) ([]byte, string, error) {
	var manifestBytes []byte
	var digest string

	err := r.db.View(func(tx *bolt.Tx) error {
		tagsBucket := tx.Bucket(bucketTags)
		manifestsBucket := tx.Bucket(bucketManifests)

		if strings.HasPrefix(reference, "sha256:") {
			// Reference is a digest — look up directly in manifests bucket.
			digest = reference
		} else {
			// Reference is a tag — resolve to digest via tags bucket.
			if tagsBucket == nil {
				return ErrManifestNotFound
			}
			tagKey := []byte(repo + ":" + reference)
			v := tagsBucket.Get(tagKey)
			if v == nil {
				return ErrManifestNotFound
			}
			digest = string(v)
		}

		if manifestsBucket == nil {
			return ErrManifestNotFound
		}
		data := manifestsBucket.Get([]byte(digest))
		if data == nil {
			return ErrManifestNotFound
		}
		// Copy the bytes since BBolt values are only valid within the
		// transaction.
		manifestBytes = make([]byte, len(data))
		copy(manifestBytes, data)
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrManifestNotFound) {
			return nil, "", ErrManifestNotFound
		}
		return nil, "", fmt.Errorf("getting manifest %s/%s: %w", repo, reference, err)
	}

	return manifestBytes, digest, nil
}

// BlobPath returns the filesystem path for the given digest after verifying
// the file exists. The digest must be in the form "sha256:{hex}". If the
// blob file does not exist, ErrBlobNotFound is returned.
func (r *Registry) BlobPath(digest string) (string, error) {
	algo, hex, ok := parseDigest(digest)
	if !ok {
		return "", ErrBlobNotFound
	}

	// Layout: {blobRoot}/{algo}/{prefix}/{full_hex}
	prefix := hex[:2]
	p := filepath.Join(r.blobRoot, algo, prefix, hex)

	if _, err := os.Stat(p); err != nil {
		if os.IsNotExist(err) {
			return "", ErrBlobNotFound
		}
		return "", fmt.Errorf("checking blob %s: %w", digest, err)
	}

	return p, nil
}

// parseDigest splits a digest string like "sha256:abcdef..." into its
// algorithm and hex components. Returns false if the format is invalid
// or the hex portion contains non-hexadecimal characters.
func parseDigest(digest string) (algo, hexStr string, ok bool) {
	parts := strings.SplitN(digest, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return "", "", false
	}
	return parts[0], parts[1], true
}
