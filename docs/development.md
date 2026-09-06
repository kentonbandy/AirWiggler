# Development Notes

## Docker releases and frontend cache-busting

AirWiggler embeds the frontend (`web/`) into the Go binary. The server injects the build version into the frontend asset URLs:

```html
style.css?v=<version>
app.js?v=<version>
```

This keeps browsers and reverse proxies/CDNs from serving stale JavaScript or CSS after an update. For this to work, Docker images should be built with a unique `VERSION` build argument for each release.

Example:

```sh
docker build \
  --build-arg VERSION=v0.1.0 \
  -t kentonbandy/airwiggler:latest \
  -t kentonbandy/airwiggler:v0.1.0 \
  .

docker push kentonbandy/airwiggler:latest
docker push kentonbandy/airwiggler:v0.1.0
```

PowerShell:

```powershell
docker build `
  --build-arg VERSION=v0.1.0 `
  -t kentonbandy/airwiggler:latest `
  -t kentonbandy/airwiggler:v0.1.0 `
  .

docker push kentonbandy/airwiggler:latest
docker push kentonbandy/airwiggler:v0.1.0
```

If the build argument is omitted, the app reports `version: dev` from `/api/config`. Reusing `dev` across deployments can cause old `app.js` or `style.css` to remain cached, which may make newly deployed UI features appear broken or partially updated.
