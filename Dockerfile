## compile proxy-init utility
FROM --platform=$BUILDPLATFORM golang:1.12.9 as golang
WORKDIR /build

# cache dependencies
COPY go.mod .
COPY go.sum .
RUN go mod download

# build
COPY . .
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -o /out/linkerd2-proxy-init -mod=readonly -ldflags "-s -w" -v

## package runtime
FROM --platform=$TARGETPLATFORM debian:buster-20201117-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        iptables \
        procps \
    && rm -rf /var/lib/apt/lists/* \
    && update-alternatives --set iptables /usr/sbin/iptables-legacy \
    && update-alternatives --set ip6tables /usr/sbin/ip6tables-legacy
COPY LICENSE /linkerd/LICENSE
COPY --from=golang /out/linkerd2-proxy-init /usr/local/bin/proxy-init
ENTRYPOINT ["/usr/local/bin/proxy-init"]
