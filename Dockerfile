FROM --platform=linux/amd64 public.ecr.aws/docker/library/golang:1.21-bookworm AS BUILDER

RUN mkdir -p /app
WORKDIR /app
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY . .
RUN go build -o gigg-image-worker cmd/server/main.go


FROM --platform=linux/amd64  dpokidov/imagemagick:latest-bookworm

RUN apt-get update && apt-get install -y ca-certificates

COPY --from=BUILDER /app/gigg-image-worker server

RUN chmod +x server

ENTRYPOINT ["./server", "run"]
