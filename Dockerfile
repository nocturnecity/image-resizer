FROM --platform=linux/amd64 public.ecr.aws/docker/library/golang:1.20 AS BUILDER

RUN apt-get update && apt-get install -y ca-certificates
RUN mkdir -p /app
WORKDIR /app
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 CGO_CFLAGS_ALLOW="-Xpreprocessor" go build -o gigg-image-worker cmd/server/main.go


FROM --platform=linux/amd64  dpokidov/imagemagick:latest

RUN apt-get update && \
    apt-get install -y python3-pip curl unzip && \
    rm -rf /var/lib/apt/lists/*

RUN pip3 install awscli

COPY --from=BUILDER /app/gigg-image-worker server
COPY --from=BUILDER /app/watermark@2x.png watermark@2x.png
RUN chmod +x server

ENTRYPOINT ["./api"]
