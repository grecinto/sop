FROM golang:1.26.4-alpine AS build

WORKDIR /src

RUN apk add --no-cache git build-base

COPY go.mod go.sum ./
COPY go.work ./
COPY lib/sop/go.mod ./lib/sop/go.mod
COPY lib/sop/go.work ./lib/sop/go.work
COPY lib/sop ./lib/sop
COPY src ./src

RUN go build -o /out/sop-site ./src/backend/site

FROM alpine:3.20

RUN apk add --no-cache ca-certificates

WORKDIR /app

ENV PORT=8081 \
    SOP_CLOUD_DATA_DIR=/var/lib/sop-cloud \
    FRONTEND_DIR=/app/frontend/dist \
    DOCS_DIR=/app/docs \
    datapath=/var/lib/sop-cloud

COPY --from=build /out/sop-site /app/sop-site
COPY src/frontend/dist /app/frontend/dist
COPY src/docs-site/build /app/docs

EXPOSE 8081

CMD ["/app/sop-site"]
