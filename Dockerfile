FROM --platform=linux/amd64 public.ecr.aws/docker/library/golang:1.20 AS BUILDER

RUN mkdir -p /app
WORKDIR /app
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 CGO_CFLAGS_ALLOW="-Xpreprocessor" go build -o gigg-image-worker cmd/server/main.go


FROM --platform=linux/amd64  dpokidov/imagemagick:7.1.1-24-bookworm

RUN apt-get update && apt-get install -y ca-certificates

COPY --from=BUILDER /app/gigg-image-worker server

RUN chmod +x server

ENTRYPOINT ["./server", "run"]
