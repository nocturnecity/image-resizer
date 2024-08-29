FROM --platform=linux/amd64 public.ecr.aws/docker/library/golang:1.22-bookworm AS BUILDER

RUN mkdir -p /app
WORKDIR /app
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY . .
RUN go build -o gigg-image-worker cmd/server/main.go


FROM --platform=linux/amd64  dpokidov/imagemagick:latest-bookworm

ENV DEBIAN_FRONTEND=noninteractive

ARG IM_VERSION=7.1.1-36

RUN apt-get -y update && \
    apt-get -y upgrade && \
    apt-get install -y --no-install-recommends git make pkg-config autoconf curl cmake clang libomp-dev ca-certificates automake
    # Building ImageMagick
RUN git clone -b ${IM_VERSION} --depth 1 https://github.com/ImageMagick/ImageMagick.git && \
    cd ImageMagick && \
    LIBS="-lsharpyuv" ./configure --without-magick-plus-plus --disable-docs --disable-static --disable-hdri --with-tiff --with-jxl --with-tcmalloc && \
    make && make install && \
    ldconfig /usr/local/lib && \
    apt-get remove --autoremove --purge -y make cmake clang clang-14 curl yasm git autoconf automake pkg-config libpng-dev libjpeg62-turbo-dev libde265-dev libx265-dev libxml2-dev libtiff-dev libfontconfig1-dev libfreetype6-dev liblcms2-dev libsdl1.2-dev libgif-dev libbrotli-dev && \
    rm -rf /var/lib/apt/lists/* && \
    rm -rf /ImageMagick

COPY --from=BUILDER /app/gigg-image-worker server

RUN chmod +x server

ENTRYPOINT ["./server", "run"]
