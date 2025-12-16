# syntax=docker/dockerfile:1.4

##
## Go
##

# Cross compile from native platform to target arch
FROM --platform=$BUILDPLATFORM golang:1.25.5-alpine as go
WORKDIR /build
COPY --link go.mod go.sum .
COPY --link ./proxy-init ./proxy-init
COPY --link ./pkg ./pkg
RUN go mod download
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH GO111MODULE=on \
    go build -o /out/linkerd2-proxy-init -mod=readonly -ldflags "-s -w" -v ./proxy-init

##
## Runtime
##

FROM --platform=$TARGETPLATFORM alpine:3.23.0 as runtime
RUN apk add iptables-legacy iptables libcap && \
    touch /run/xtables.lock && \
    chmod 0666 /run/xtables.lock

COPY --link --from=go /out/linkerd2-proxy-init /usr/local/bin/proxy-init

# Set sys caps for iptables utilities and proxy-init
RUN setcap cap_net_raw,cap_net_admin+eip /usr/sbin/xtables-legacy-multi && \
    setcap cap_net_raw,cap_net_admin+eip /usr/sbin/xtables-nft-multi && \
    setcap cap_net_raw,cap_net_admin+eip /usr/local/bin/proxy-init

USER 65534
ENTRYPOINT ["/usr/local/bin/proxy-init"]
