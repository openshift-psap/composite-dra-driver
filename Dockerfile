FROM golang:1.26 AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /composite-dra-driver ./cmd/driver

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /composite-dra-driver /composite-dra-driver
USER 65532:65532
ENTRYPOINT ["/composite-dra-driver"]
