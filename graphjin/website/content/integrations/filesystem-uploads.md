---
title: "Filesystem Tables And Uploads"
description: "Expose local, S3-compatible, and GCS object storage as GraphQL tables."
nav_group: "integrations"
doc_kind: "guide"
weight: 40
---

## Filesystem tables

```yaml
sources:
  - name: documents
    kind: file
    backend: s3
    bucket: app-documents
    prefix: tenants/${account_id}
    presign_ttl: 15m
    capabilities:
      files.list: true
      files.read: true
      files.write: false
```

Every filesystem table exposes stable columns such as `key`, `size`, `content_type`, `etag`, `modified_at`, `url`, and `data`.

```graphql
query {
  documents(prefix: "reports/", first: 20, order_by: { key: asc }) {
    key
    size
    content_type
    url
  }
  documents_cursor
}
```

{{< svg "filesystem-pipeline" "Filesystem tables pipeline" >}}

{{< verified by="TestIntrospectionIncludesFilesystemRemoteCursorField" file="core/intro_test.go" line="328" >}}
{{< verified by="TestBridge_LocalWritesInvalidateFilesystemCacheRefs" file="core/fstable_bridge_test.go" line="473" >}}

## Uploads

Uploads accept multipart requests on `/api/v1/graphql`. Files can either be inlined as base64 variables or streamed into a configured filesystem table.

```graphql
mutation ($file: Upload!) {
  documents(insert: { file: $file }) {
    key
    size
    url
  }
}
```

When `uploads.storage` names a filesystem table, the upload handler streams the file into the backend and replaces the variable with object metadata instead of base64 file data.

{{< verified by="TestGenerateUploadKey_DateMarker" file="serv/upload_storage_test.go" line="122" >}}
{{< verified by="TestMIMEAllowed_GlobAndExact" file="serv/upload_test.go" line="173" >}}

## Policy

Filesystems participate in source capabilities and read-only policy. Use read-only defaults for discovery surfaces and enable writes deliberately.

Read-only filesystems block managed writes and watchers. S3/GCS presigned URLs are cache-bounded so GraphJin does not serve stale URLs past their TTL.

{{< verified by="TestBridge_ReadOnlyFilesystemBlocksManagedWrites" file="core/fstable_bridge_test.go" line="522" >}}
{{< verified by="TestFilesystemFragmentCacheOptions" file="core/cache_response_test.go" line="141" >}}
