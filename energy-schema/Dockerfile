ARG BUILD_FROM

FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /energy-schema .

FROM ${BUILD_FROM}
COPY --from=build /energy-schema /usr/bin/energy-schema
CMD [ "/usr/bin/energy-schema" ]
