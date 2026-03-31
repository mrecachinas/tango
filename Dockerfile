FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=unknown
RUN CGO_ENABLED=0 go build \
    -ldflags "-s -w -X tango/internal/version.Version=${VERSION} -X tango/internal/version.Commit=${COMMIT}" \
    -o /tango ./cmd/tango

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /tango /usr/local/bin/tango
ENTRYPOINT ["tango"]
