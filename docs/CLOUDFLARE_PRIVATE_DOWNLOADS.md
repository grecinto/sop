# Private Cloudflare downloads with short-lived signed URLs

The existing download flow already supports a private artifact source. The site checks entitlement in [src/backend/siteapi/handlers.go](src/backend/siteapi/handlers.go), returns a `download_url`, and the UI in [src/frontend/src/App.tsx](src/frontend/src/App.tsx) opens it.
TODO: When the daemon release is published and the Cloudflare-hosted asset is ready, replace the current fallback URL in [src/backend/siteapi/handlers.go](src/backend/siteapi/handlers.go) with the Cloudflare object URL or a short-lived signed URL generated for that release.
To make the binary private while still allowing a secure download experience, generate a short-lived signed URL from your storage backend before returning it.

## Recommended approach

Use Cloudflare R2 as the storage backend and sign a `GetObject` URL for a short expiry window (for example 5–15 minutes).

### Flow

1. Upload the binary to a private R2 bucket.
2. After the user is authenticated and entitled, create a signed URL for the object.
3. Return that signed URL from the backend download endpoint.
4. The frontend opens the URL exactly as it already does today.

## Backend hook

The integration point is the existing endpoint in [src/backend/siteapi/handlers.go](src/backend/siteapi/handlers.go):

- `GET /api/auth/download`
- It already performs entitlement checks.
- It already returns a JSON payload with `download_url`.

The only change needed is to replace the fallback/public URL with a signed URL from R2.

## Example implementation sketch

```go
func buildSignedDownloadURL(ctx context.Context, objectKey string) (string, error) {
    // Use Cloudflare R2-compatible S3 signing.
    // The exact implementation depends on your SDK choice.
    // Example idea:
    // 1. Create an S3 client with R2 endpoint + credentials.
    // 2. Presign a GetObject request with a short expiry.
    // 3. Return the signed URL.
    return "https://<account>.r2.cloudflarestorage.com/<bucket>/" + objectKey, nil
}
```

In the handler, after entitlement is confirmed:

```go
signedURL, err := buildSignedDownloadURL(r.Context(), "releases/sop-agent")
if err != nil {
    writeJSONError(w, http.StatusInternalServerError, "unable to create download url")
    return
}

_ = json.NewEncoder(w).Encode(map[string]any{
    "status":       "ok",
    "available":    true,
    "download_url": signedURL,
    "plan":         user.SubscriptionPlan,
})
```

## Environment variables

Add a small set of environment variables for the signing layer:

```bash
R2_ENDPOINT=https://<account-id>.r2.cloudflarestorage.com
R2_REGION=auto
R2_BUCKET=<bucket-name>
R2_ACCESS_KEY_ID=<access-key>
R2_SECRET_ACCESS_KEY=<secret>
R2_DOWNLOAD_OBJECT=releases/sop-agent
R2_DOWNLOAD_TTL_SECONDS=600
```

## Security notes

- Keep the bucket private.
- Use short expiry values such as 5–15 minutes.
- Only sign URLs after the user has passed the entitlement check.
- Consider binding the URL to a specific user or IP if you need tighter control.
- Avoid returning long-lived public URLs.

## Why this fits the current app

No frontend changes are required. The UI already handles the returned `download_url` and opens it in the browser. The private part is entirely on the backend side.
