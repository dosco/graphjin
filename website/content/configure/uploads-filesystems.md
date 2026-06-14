---
title: "Uploads And Filesystems"
description: "Configure multipart uploads and object-store-backed filesystem tables."
nav_group: "configure"
doc_kind: "reference"
weight: 50
---

## Uploads

```yaml
uploads:
  enabled: true
  max_size: 26214400
  allowed_mime: ["image/*", "application/pdf"]
  storage: documents
  storage_key_prefix: "{date}/"
```

Uploads can be passed as base64 variables or streamed into a filesystem table.

When `storage` is set, the multipart parser writes the upload to that filesystem source and replaces the GraphQL variable with object metadata: `key`, `content_type`, `size`, `url`, `etag`, and `modified_at`.

{{< verified by="TestGenerateUploadKey_DateMarker" file="serv/upload_storage_test.go" line="122" >}}
{{< verified by="TestParseMultipart_MIMEAllowlist" file="serv/upload_test.go" line="156" >}}

## Filesystem backends

```yaml
sources:
  - name: documents
    kind: file
    backend: local
    root: ./data/documents
    presign_ttl: 15m
    capabilities:
      files.list: true
      files.read: true
      files.write: false
```

Backends include local, S3-compatible stores, and GCS. Slim builds can omit cloud SDK weight when those backends are not needed.

```yaml
sources:
  - name: media
    kind: file
    backend: s3
    bucket: app-media
    region: us-east-1
    prefix: uploads/
    public_base_url: https://cdn.example.com/media/
    max_list_page_size: 500
```

## Query files

```graphql
query {
  documents(prefix: "invoices/", first: 25, order_by: { key: asc }) {
    key
    size
    content_type
    url
  }
  documents_cursor
}
```

Filesystem tables support list, stat, get, put, delete, presign, cursor pagination, and source-scoped cache invalidation.

{{< verified by="TestIntrospectionIncludesFilesystemRemoteCursorField" file="core/intro_test.go" line="328" >}}
{{< verified by="TestBridge_ReadOnlyFilesystemBlocksManagedWrites" file="core/fstable_bridge_test.go" line="522" >}}
