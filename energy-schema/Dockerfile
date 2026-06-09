ARG BUILD_FROM

FROM golang:1.23-alpine AS build
WORKDIR /src
# Stdlib-only module: go.mod is enough, no go.sum to copy.
COPY go.mod ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /energy-schema ./cmd/energy-schema

FROM ${BUILD_FROM}
COPY --from=build /energy-schema /usr/bin/energy-schema
CMD [ "/usr/bin/energy-schema" ]
