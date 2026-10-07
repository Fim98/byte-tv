ARG GO_IMAGE=golang:1.26-bookworm
ARG RUNTIME_IMAGE=debian:bookworm-slim
FROM ${GO_IMAGE} AS build
ENV GOPROXY=https://goproxy.cn,direct GOSUMDB=off
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/juku .

FROM ${RUNTIME_IMAGE}
ENV TZ=Asia/Shanghai
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates ffmpeg tzdata tini curl && rm -rf /var/lib/apt/lists/* && groupadd --gid 1000 juku && useradd --uid 1000 --gid juku --no-create-home juku && mkdir -p /data /downloads && chown juku:juku /data /downloads
COPY --from=build /out/juku /usr/local/bin/juku
USER 1000:1000
WORKDIR /data
VOLUME ["/data", "/downloads"]
EXPOSE 8999
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 CMD curl --noproxy '*' --fail --silent --output /dev/null http://127.0.0.1:8999/ || exit 1
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/juku"]
CMD ["-open=false", "-listen", "0.0.0.0:8999", "-data-dir", "/data", "-out", "/downloads", "-ffmpeg", "/usr/bin/ffmpeg"]
