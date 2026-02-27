# XTemp Acceptance Checklist

This document defines pre-deployment acceptance for the current release.

## Scope

1. Test upload by `PUT` and `POST`.
2. Test corresponding downloads.
3. Test automatic deletion (local storage only).
4. Test manual deletion for both upload methods.

## Environment

- Target host: `192.168.1.24`
- Service URL: `http://127.0.0.1:5800`
- Container: `xtemp-local-test`
- Storage type for this acceptance: `local`

## Deployment Command (local acceptance mode)

```sh
docker rm -f xtemp-local-test >/dev/null 2>&1 || true
docker build -q -t xtemp-local:codex .
docker run -d --name xtemp-local-test -p 5800:5000 \
  -e STORAGE_TYPE=local \
  -e XTEMP_STORAGE_PATH=/tmp/xtemp-store \
  -e MAX_UPLOAD_SIZE=524288000 \
  -e XTEMP_CONFIG_API_PASSWORD=verify-pass \
  -e XTEMP_RETENTION_SECONDS=60 \
  -e XTEMP_CLEANUP_INTERVAL_SECONDS=10 \
  xtemp-local:codex
```

## Acceptance Steps

### A. POST Upload + Download + Manual Delete

1. Upload file with `POST`:
```sh
curl -sS -H "User-Agent: Mozilla/5.0" -X POST -F "file=@/tmp/post_case.txt" "$BASE/"
```
2. Download from returned `url` and compare content.
3. Delete with `DELETE <url>`.
4. Verify download returns `404` after deletion.

### B. PUT Upload + Download + Manual Delete

1. Upload file with `PUT`:
```sh
curl -sS -H "User-Agent: Mozilla/5.0" -X PUT --data-binary @/tmp/put_case.txt "$BASE/custom-put-name.txt"
```
2. Download from returned `url` and compare content.
3. Delete with `DELETE <url>`.
4. Verify download returns `404` after deletion.

### C. Automatic Deletion (Local Only)

1. Upload a file.
2. Confirm immediate download is `200`.
3. Wait longer than retention (`> 60s` with current acceptance config).
4. Verify download is `404`.
5. Confirm cleanup log line exists in container logs.

## Result Record

- [x] A. POST flow passed
- [x] B. PUT flow passed
- [x] C. Local auto-cleanup passed
- [x] No regression observed in basic upload/download/delete behavior

### Execution Notes

- A. POST:
  - Download compare: `OK`
  - Manual delete response: success
  - After delete HTTP code: `404`
- B. PUT:
  - Download compare: `OK`
  - Manual delete response: success
  - After delete HTTP code: `404`
- C. Local auto-cleanup:
  - Retention policy response: `retention_seconds=60`
  - Immediate download code: `200`
  - After 75s download code: `404`
  - Cleanup log found in container output
