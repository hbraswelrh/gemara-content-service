// seed populates a local OCI storage directory with sample Gemara content
// for development and testing. The seed data matches the complyctl mock
// OCI registry so that complyctl can be tested against gemara-content-service.
//
// Usage:
//
//	go run ./hack/seed --output /tmp/oci-store
package main

import (
	"crypto/sha256"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	bolt "go.etcd.io/bbolt"
)

//go:embed testdata/*.yaml
var seedData embed.FS

const (
	gemaraCatalogType  = "application/vnd.gemara.catalog.v1+yaml"
	gemaraGuidanceType = "application/vnd.gemara.guidance.v1+yaml"
	gemaraPolicyType   = "application/vnd.gemara.policy.v1+yaml"
)

type descriptor struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

type manifest struct {
	SchemaVersion int          `json:"schemaVersion"`
	MediaType     string       `json:"mediaType"`
	Config        descriptor   `json:"config"`
	Layers        []descriptor `json:"layers"`
}

type layerDef struct {
	mediaType string
	data      []byte
}

type artifactDef struct {
	repo   string
	tags   []string
	layers []layerDef
}

func main() {
	var output string
	flag.StringVar(&output, "output", "/tmp/oci-store", "Output storage root directory")
	flag.Parse()

	blobRoot := filepath.Join(output, "blobs")
	dbPath := filepath.Join(output, "index.db")

	cisCatalog, err := seedData.ReadFile("testdata/cis-fedora-l1-server-catalog.yaml")
	if err != nil {
		log.Fatalf("failed to load CIS Fedora catalog seed data: %v", err)
	}
	cisPolicy, err := seedData.ReadFile("testdata/cis-fedora-l1-server-policy.yaml")
	if err != nil {
		log.Fatalf("failed to load CIS Fedora policy seed data: %v", err)
	}

	artifacts := []artifactDef{
		{
			repo: "policies/nist-800-53-r5",
			tags: []string{"v1.0.0", "latest"},
			layers: []layerDef{
				{mediaType: gemaraCatalogType, data: []byte(`title: NIST SP 800-53 Rev 5
metadata:
  id: nist-800-53-r5
  description: Security and privacy controls for information systems
  author:
    id: nist
    name: NIST
    type: Human
families:
  - id: access-control
    title: Access Control
    description: Controls related to access management
controls:
  - id: AC-1
    title: Access Control Policy
    objective: Establish and maintain access control policy
    family: access-control
    assessment-requirements:
      - id: AC-1-ar
        text: Access control policy MUST be documented and maintained
        applicability:
          - All systems
  - id: AC-2
    title: Account Management
    objective: Manage information system accounts
    family: access-control
    assessment-requirements:
      - id: AC-2-ar
        text: System accounts MUST be properly managed
        applicability:
          - All systems
`)},
				{mediaType: gemaraPolicyType, data: []byte(`title: NIST SP 800-53 Rev 5 Policy
metadata:
  id: nist-800-53-r5-policy
  description: Automated evaluation policy for NIST SP 800-53 Rev 5
  author:
    id: complytime
    name: ComplyTime
    type: Software
  mapping-references:
    - id: nist-800-53-r5
      title: NIST SP 800-53 Rev 5
      version: "5.0"
contacts:
  responsible:
    - name: System Administrator
  accountable:
    - name: Security Team
scope:
  in:
    technologies:
      - Information Systems
imports:
  catalogs:
    - reference-id: nist-800-53-r5
adherence:
  evaluation-methods:
    - type: automated
      executor:
        id: test
        name: Test Evaluator
        type: Software
  assessment-plans:
    - id: AC-1-impl
      requirement-id: AC-1-ar
      frequency: on-demand
      evaluation-methods:
        - type: automated
    - id: AC-2-impl
      requirement-id: AC-2-ar
      frequency: on-demand
      evaluation-methods:
        - type: automated
`)},
			},
		},
		{
			repo: "policies/cis-benchmark",
			tags: []string{"v2.0.0", "latest"},
			layers: []layerDef{
				{mediaType: gemaraCatalogType, data: []byte(`title: CIS Benchmark
metadata:
  id: cis-benchmark
  description: Center for Internet Security Benchmark controls
  author:
    id: cis
    name: CIS
    type: Human
families:
  - id: filesystem
    title: Filesystem Configuration
    description: Controls for filesystem hardening
controls:
  - id: CIS-1.1
    title: Filesystem Configuration
    objective: Harden filesystem configuration
    family: filesystem
    assessment-requirements:
      - id: CIS-1.1-ar
        text: Filesystem MUST be properly configured
        applicability:
          - All systems
`)},
			},
		},
		{
			repo: "catalogs/osps-b",
			tags: []string{"v1.0.0", "latest"},
			layers: []layerDef{
				{mediaType: gemaraCatalogType, data: []byte(`title: Open Source Project Security Baseline
metadata:
  id: osps-b
  description: Security baseline controls for open source projects
  author:
    id: openssf
    name: OpenSSF
    type: Human
families:
  - id: quality-assurance
    title: Quality Assurance
    description: Controls ensuring software quality and security
controls:
  - id: OSPS-QA-07.01
    title: Quality Assurance Control
    objective: Ensure quality assurance processes are in place
    family: quality-assurance
    assessment-requirements:
      - id: OSPS-QA-07.01-ar
        text: Quality assurance controls MUST be implemented
        applicability:
          - Open source projects
`)},
			},
		},
		{
			repo: "guidance/nist",
			tags: []string{"v1.0.0", "latest"},
			layers: []layerDef{
				{mediaType: gemaraGuidanceType, data: []byte(`title: NIST Security Guidance
metadata:
  id: nist-guidance
  description: NIST security guidance for information systems
  author:
    id: nist
    name: NIST
    type: Human
type: Standard
families:
  - id: access-control
    title: Access Control
    description: Guidelines related to access management
guidelines:
  - id: nist-guide-ac
    title: Access Control Guidance
    objective: Provide guidance on access control implementation
    family: access-control
`)},
			},
		},
		{
			repo: "policies/cis-fedora-l1-server",
			tags: []string{"v1.0.0", "latest"},
			layers: []layerDef{
				{mediaType: gemaraCatalogType, data: cisCatalog},
				{mediaType: gemaraPolicyType, data: cisPolicy},
			},
		},
	}

	emptyConfig := []byte("{}")
	emptyConfigDigest := digestOf(emptyConfig)

	if err := writeBlob(blobRoot, emptyConfigDigest, emptyConfig); err != nil {
		log.Fatalf("writing empty config blob: %v", err)
	}

	os.Remove(dbPath)

	db, err := bolt.Open(dbPath, 0600, nil)
	if err != nil {
		log.Fatalf("opening bbolt: %v", err)
	}
	defer db.Close()

	err = db.Update(func(tx *bolt.Tx) error {
		tagsBucket, err := tx.CreateBucketIfNotExists([]byte("tags"))
		if err != nil {
			return err
		}
		manifestsBucket, err := tx.CreateBucketIfNotExists([]byte("manifests"))
		if err != nil {
			return err
		}

		for _, a := range artifacts {
			layerDescs := make([]descriptor, 0, len(a.layers))
			for _, l := range a.layers {
				contentDigest := digestOf(l.data)
				if err := writeBlob(blobRoot, contentDigest, l.data); err != nil {
					return fmt.Errorf("writing blob for %s: %w", a.repo, err)
				}
				layerDescs = append(layerDescs, descriptor{
					MediaType: l.mediaType,
					Digest:    contentDigest,
					Size:      int64(len(l.data)),
				})
			}

			m := manifest{
				SchemaVersion: 2,
				MediaType:     "application/vnd.oci.image.manifest.v1+json",
				Config: descriptor{
					MediaType: "application/vnd.oci.empty.v1+json",
					Digest:    emptyConfigDigest,
					Size:      int64(len(emptyConfig)),
				},
				Layers: layerDescs,
			}

			manifestBytes, err := json.Marshal(m)
			if err != nil {
				return fmt.Errorf("marshaling manifest for %s: %w", a.repo, err)
			}

			manifestDigest := digestOf(manifestBytes)

			if err := writeBlob(blobRoot, manifestDigest, manifestBytes); err != nil {
				return fmt.Errorf("writing manifest blob for %s: %w", a.repo, err)
			}

			if err := manifestsBucket.Put([]byte(manifestDigest), manifestBytes); err != nil {
				return err
			}

			for _, tag := range a.tags {
				tagKey := a.repo + ":" + tag
				if err := tagsBucket.Put([]byte(tagKey), []byte(manifestDigest)); err != nil {
					return err
				}
				log.Printf("  %s:%s -> %s", a.repo, tag, manifestDigest[:30]+"...")
			}
		}

		return nil
	})
	if err != nil {
		log.Fatalf("populating database: %v", err)
	}

	log.Printf("Storage root ready at %s", output)
	log.Printf("Run the server with: go run ./cmd/compass --storage-root %s --skip-tls", output)
}

func digestOf(data []byte) string {
	h := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", h)
}

func writeBlob(blobRoot, digest string, data []byte) error {
	hex := digest[len("sha256:"):]
	prefix := hex[:2]
	dir := filepath.Join(blobRoot, "sha256", prefix)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, hex), data, 0600)
}
