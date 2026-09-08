# ---- build stage ----
FROM golang:1.26-alpine AS build
WORKDIR /src

# Cache dependencies first.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Everything (templates, static assets, migrations) is embedded at compile
# time, so the output is a single self-contained static binary.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/markitdown ./cmd/server
RUN mkdir /data

# ---- runtime stage: distroless, no shell, non-root, ~15MB image ----
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/markitdown /markitdown
COPY --from=build --chown=nonroot:nonroot /data /data
# Hugging Face Spaces (Docker SDK) requires the container to listen on 7860.
# These are defaults only: HF Space secrets and `docker run --env-file .env`
# override them (local .env uses PORT=8909).
ENV PORT=7860
ENV DATABASE_URL=file:/data/markitdown.db?_busy_timeout=5000&_foreign_keys=on
EXPOSE 7860
USER nonroot:nonroot
ENTRYPOINT ["/markitdown"]
