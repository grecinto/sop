# Root httpserver launcher

This launcher keeps the legacy SOP Data Manager entrypoint distinct from the new product site.

Use it to start the Data Manager UI:

```bash
go run ./tools/httpserver
```

Use the dedicated site backend to launch the new React site UI:

```bash
PORT=8081 go run ./src/backend/site
```
