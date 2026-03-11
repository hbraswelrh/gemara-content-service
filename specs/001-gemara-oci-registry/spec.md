# Feature Specification: Gemara OCI Content Delivery Registry

**Feature Branch**: `001-gemara-oci-registry`  
**Created**: 2026-02-11  
**Status**: Draft  
**Input**: User description: Add a Gemara content delivery API implemented as an OCI Distribution
Spec v2 server. Clients can discover and download Gemara compliance YAML (L1 guidance, L2 catalog,
L3 policy) as OCI artifacts. Read-only endpoints: handshake GET /v2/, list repos GET /v2/_catalog,
list tags GET /v2/{repo}/tags/list, get manifest GET /v2/{repo}/manifests/{reference}, get blob
GET /v2/{repo}/blobs/{digest}. Storage: blobs on filesystem (content-addressable by sha256),
tag/digest index in embedded BBolt. Return 404 with OCI-style JSON errors when tag or digest is
missing. No auth in this phase. Support Gemara media types in manifests (guidance, catalog, policy).
Align with existing complybeacon-api.yaml OCI paths where defined.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Discover Available Gemara Content (Priority: P1)

A compliance tool operator wants to see what Gemara content is available in the
registry so they can decide which policies, catalogs, or guidance to download.

**Why this priority**: Discovery is the entry point. Without knowing what exists,
no other interaction is useful.

**Independent Test**: Can be tested by making HTTP requests to the handshake and
catalog endpoints and verifying the response lists known repositories.

**Acceptance Scenarios**:

1. **Given** the registry is running with pre-loaded content, **When** a client
   sends `GET /v2/`, **Then** the server returns `200 OK` with header
   `Docker-Distribution-API-Version: registry/2.0`.
2. **Given** the registry contains repositories (e.g. `policies/rhel11`,
   `catalogs/pci-dss`, `guidance/nist`), **When** a client sends
   `GET /v2/_catalog`, **Then** the server returns a JSON object with a
   `repositories` array listing all repository names.
3. **Given** a repository `policies/rhel11` has tags `v1.0.0` and `v1.1.0`,
   **When** a client sends `GET /v2/policies/rhel11/tags/list`, **Then** the
   server returns `{"name": "policies/rhel11", "tags": ["v1.0.0", "v1.1.0"]}`.

---

### User Story 2 - Download a Specific Version of Gemara Content (Priority: P1)

A compliance tool (or human operator using curl/oras) wants to download a
specific version of a Gemara artifact (e.g. an L2 catalog at tag `v1.0.0`) so
they can use it locally for policy evaluation.

**Why this priority**: Downloading content is the core value of the registry. It
is co-equal with discovery.

**Independent Test**: Can be tested by requesting a manifest by tag, extracting
the blob digest from the manifest layers, then fetching the blob and verifying
it matches the expected YAML content.

**Acceptance Scenarios**:

1. **Given** repository `catalogs/osps-b` has tag `v1.0.0`, **When** a client
   sends `GET /v2/catalogs/osps-b/manifests/v1.0.0`, **Then** the server
   returns the OCI manifest JSON with `Content-Type:
   application/vnd.oci.image.manifest.v1+json`.
2. **Given** a manifest layer descriptor has digest `sha256:abc123...` and media
   type `application/vnd.gemara.catalog.v1+yaml`, **When** a client sends
   `GET /v2/catalogs/osps-b/blobs/sha256:abc123...`, **Then** the server
   streams the raw YAML file with `Content-Type: application/octet-stream`.
3. **Given** repository `catalogs/osps-b` has a manifest stored by digest,
   **When** a client sends
   `GET /v2/catalogs/osps-b/manifests/sha256:<digest>`, **Then** the server
   returns the same manifest as when fetched by tag.

---

### User Story 3 - Handle Missing Content Gracefully (Priority: P2)

A client requests a repository, tag, or blob that does not exist. The registry
must return a clear, OCI-compliant error so the client can handle it
programmatically.

**Why this priority**: Robust error handling is essential for any registry
client, but it depends on the core read path (US1/US2) being implemented first.

**Independent Test**: Can be tested by requesting non-existent repositories,
tags, and digests and verifying the error response format and HTTP status code.

**Acceptance Scenarios**:

1. **Given** no repository named `policies/nonexistent` exists, **When** a
   client sends `GET /v2/policies/nonexistent/tags/list`, **Then** the server
   returns `404` with JSON body
   `{"errors": [{"code": "NAME_UNKNOWN", "message": "..."}]}`.
2. **Given** repository `policies/rhel11` exists but has no tag `v99.0.0`,
   **When** a client sends
   `GET /v2/policies/rhel11/manifests/v99.0.0`, **Then** the server returns
   `404` with `{"errors": [{"code": "MANIFEST_UNKNOWN", "message": "..."}]}`.
3. **Given** no blob with digest `sha256:0000...` exists, **When** a client
   sends `GET /v2/policies/rhel11/blobs/sha256:0000...`, **Then** the server
   returns `404` with `{"errors": [{"code": "BLOB_UNKNOWN", "message": "..."}]}`.

---

### User Story 4 - Gemara Media Types in Manifests (Priority: P2)

A compliance tool needs to distinguish between Gemara content types (L1 guidance,
L2 catalog, L3 policy) within a manifest so it can handle each layer
appropriately.

**Why this priority**: Media type differentiation is important for correct client
behavior, but the registry can serve content without it being validated on the
client side.

**Independent Test**: Can be tested by fetching a manifest and inspecting the
`mediaType` field of each layer descriptor.

**Acceptance Scenarios**:

1. **Given** a manifest for an L3 policy artifact, **When** a client fetches the
   manifest, **Then** the layer descriptor's `mediaType` is
   `application/vnd.gemara.policy.v1+yaml`.
2. **Given** a manifest for an L2 catalog artifact, **When** a client fetches the
   manifest, **Then** the layer descriptor's `mediaType` is
   `application/vnd.gemara.catalog.v1+yaml`.
3. **Given** a manifest for an L1 guidance artifact, **When** a client fetches the
   manifest, **Then** the layer descriptor's `mediaType` is
   `application/vnd.gemara.guidance.v1+yaml`.

---

### Edge Cases

- What happens when the blob storage directory does not exist or is empty at
  startup? The server should start successfully and return empty catalog/tag
  lists until content is loaded.
- What happens when a BBolt database file does not exist at startup? The server
  should create a new, empty database.
- What happens when a manifest references a blob digest that is missing from the
  filesystem? The server should return `404 BLOB_UNKNOWN` for blob requests even
  if the manifest exists.
- What happens when two requests arrive simultaneously for different tags? The
  server must handle concurrent reads safely (BBolt read transactions are
  parallel-safe).

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST implement the OCI Distribution Spec v2 handshake at
  `GET /v2/`, returning `200 OK` with header
  `Docker-Distribution-API-Version: registry/2.0`.
- **FR-002**: System MUST list all repository names via `GET /v2/_catalog`,
  derived from the tags stored in the index.
- **FR-003**: System MUST list all tags for a given repository via
  `GET /v2/{name}/tags/list`, returning the repository name and tag array.
- **FR-004**: System MUST return an OCI manifest via
  `GET /v2/{name}/manifests/{reference}`, supporting both tag and digest as
  reference. When reference is a tag, the system looks up the digest in the
  index; when reference is a digest, the system looks up the manifest directly.
- **FR-005**: System MUST stream a raw blob via
  `GET /v2/{name}/blobs/{digest}`, reading from the content-addressable
  filesystem store.
- **FR-006**: System MUST store blobs on the filesystem in a content-addressable
  layout: `{storage_root}/blobs/sha256/{prefix}/{full_hex}`, where `{prefix}` is
  the first two hex characters of the digest.
- **FR-007**: System MUST use an embedded BBolt database as the metadata index,
  with a `tags` bucket (key: `{repo}:{tag}`, value: digest string) and a
  `manifests` bucket (key: digest string, value: OCI manifest JSON). Manifest
  JSON MUST be stored and served as raw bytes. The digest stored as the key in
  the `manifests` bucket MUST be the sha256 hash of the exact byte sequence
  stored as the value. The server MUST NOT unmarshal and re-serialize manifest
  JSON when serving responses, as re-serialization may alter whitespace or key
  ordering, producing a different digest.
- **FR-008**: System MUST use BBolt read transactions (`db.View`) for all read
  operations to ensure parallel safety.
- **FR-009**: System MUST return OCI-compliant JSON error bodies for 404
  responses, using codes `NAME_UNKNOWN`, `MANIFEST_UNKNOWN`, and `BLOB_UNKNOWN`.
- **FR-010**: System MUST set `Content-Type:
  application/vnd.oci.image.manifest.v1+json` header on manifest responses.
- **FR-011**: System MUST set `Content-Type: application/octet-stream` on blob
  responses.
- **FR-012**: Manifests MUST support Gemara-specific media types in layer
  descriptors: `application/vnd.gemara.policy.v1+yaml` (L3),
  `application/vnd.gemara.catalog.v1+yaml` (L2), and
  `application/vnd.gemara.guidance.v1+yaml` (L1).
- **FR-013**: System MUST NOT require authentication for any endpoint in this
  phase.
- **FR-014**: The storage root path MUST be configurable (e.g. via environment
  variable or command-line flag).

### Key Entities

- **Repository**: A named collection of tagged artifacts (e.g.
  `policies/rhel11`, `catalogs/osps-b`). Repositories have a name (string, may
  contain slashes) and zero or more tags.
- **Tag**: A human-readable version label (e.g. `v1.0.0`, `latest`) pointing to
  a specific manifest digest within a repository.
- **Manifest**: An OCI image manifest (JSON) describing one or more layers. Each
  layer has a digest, size, and media type. The manifest also has a config
  descriptor.
- **Blob**: A content-addressable file on disk, identified by its sha256 digest.
  Blobs contain the raw Gemara YAML content.
- **Index (BBolt)**: An embedded key-value database mapping tags to digests
  (`tags` bucket) and digests to manifest JSON (`manifests` bucket).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A client using standard OCI tooling (e.g. `oras`, `crane`, `curl`)
  can discover all available Gemara content in under 2 seconds for a registry
  with up to 100 repositories.
- **SC-002**: A client can download a specific Gemara YAML artifact (manifest +
  blob) in a single request flow (manifest then blob) without errors.
- **SC-003**: All 404 error responses conform to the OCI error format and include
  the correct error code, verifiable by automated tests.
- **SC-004**: Concurrent read requests (at least 10 simultaneous) complete
  without data corruption or server errors.
- **SC-005**: The registry starts successfully with an empty storage directory and
  returns empty catalog/tag lists, verifiable by an integration test.

## Assumptions

- This phase covers the **read-only serving path** only. The mechanism for
  loading content into the blob store and metadata index (e.g. OCI push API,
  CLI tooling, or external process) is out of scope for this feature and will
  be defined separately. The client CLI **complyctl** will be used to interact
  with the OCI API. Tests use pre-built fixture files and a pre-populated
  BBolt.
- **No pagination** is needed for catalog or tag listing in this phase;
  registries are expected to have a small number of repositories and tags.
- **HEAD** requests are supported for manifests to allow clients (e.g.
  `complyctl`) to check manifest existence and retrieve the digest without
  downloading the full body.
- Manifests may contain **multiple layers**, each pointing to a Gemara YAML
  blob with its own media type (e.g. a policy artifact with both catalog and
  policy layers).
- The OCI manifest **config** descriptor will use a minimal empty config blob
  (or `application/vnd.oci.empty.v1+json`); no Gemara-specific config metadata
  is needed in this phase.
- The OCI routes will be served by the **existing Compass Gin server** (same
  binary, same port), alongside the existing `POST /v1/enrich` endpoint.
- The OpenAPI spec file `api.yaml` defines both OCI and enrichment endpoints;
  the implementation will align with those definitions.

## Open Questions

The following architectural questions were surfaced during spec review
(2026-02-16) and will be resolved in subsequent features:

- **Ingestion mechanism**: Whether to add OCI push endpoints (simplifies
  ingestion, enables artifact signing with cosign, aligns with the standard OCI
  ecosystem) vs. keep the API read-only with a separate ingestion path (CLI
  subcommand, external loader, etc.).
- **Storage backend**: Whether to use OCI Image Layout on the filesystem (like
  zot — no embedded database, simpler, standard tooling compatible) vs. BBolt
  for the metadata index (faster indexed lookups, but introduces file-lock and
  hot-reload concerns).
- **Authentication and authorization**: Model for a future phase — OIDC (e.g.
  GitHub Actions workload identity for CI push), mTLS, or HTTP basic auth.
  Includes per-endpoint access control (e.g. anonymous pull, authenticated
  push).

## Clarifications

### Session 2026-02-13

- Q: Is a seed/loader CLI needed as part of this feature to populate storage? → A: No; complyctl is the client CLI that will talk to the OCI API. Tests use pre-built fixtures.
- Q: Are repository names enforced to a specific structure? → A: No; repo names are freeform paths (may have any depth, e.g. `type/name`, `type/subtype/name`). The Gemara content type is determined by the media type in the manifest layer descriptor, not by the repo path.
