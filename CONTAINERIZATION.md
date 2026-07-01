# Containerization

The site backend is now packaged as a single container-friendly Go process that serves:

- the product site frontend at /site/
- the shared sopbackend monitorability and Swagger endpoints
- the site account APIs used by the new onboarding experience

## Runtime layout

The container honors these environment variables:

- SOP_CLOUD_DATA_DIR (preferred)
- datapath (shared SOP convention)
- SOP_DATA_DIR

When unset, the process uses ./.sop-data relative to the current working directory.

## Build and run

```bash
docker build -t sop-cloud-site .
docker run --rm -p 8081:8081 -e PORT=8081 sop-cloud-site
```

Or with Docker Compose:

```bash
docker compose up --build
```

The runtime writes its persistent config and state under the configured data directory, which is mounted to /var/lib/sop-cloud in the provided Compose file.
