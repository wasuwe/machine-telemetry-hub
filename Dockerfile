# --- build stage ---
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/telemetryhub ./cmd/telemetryhub

# --- run stage: static binary on a minimal, non-root image ---
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/telemetryhub /telemetryhub
EXPOSE 8080
ENTRYPOINT ["/telemetryhub"]
