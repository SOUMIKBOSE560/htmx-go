# ---- build stage ----
FROM golang:1.26-alpine AS build
WORKDIR /src

# Cache dependencies first.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Everything (templates, static assets, migrations) is embedded at compile
# time, so the output is a single self-contained static binary.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/pageturner ./cmd/server

# ---- runtime stage: distroless, no shell, non-root, ~15MB image ----
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/pageturner /pageturner
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/pageturner"]
