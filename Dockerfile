## Compile proxy-init utility
FROM --platform=$BUILDPLATFORM golang:1.18-alpine as build
WORKDIR /build
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY ./proxy-init ./proxy-init
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -o /out/linkerd2-proxy-init -mod=readonly -ldflags "-s -w" -v ./proxy-init

## Package runtime
FROM --platform=$TARGETPLATFORM alpine:3.16.2
RUN apk add iptables libcap && \
    touch /run/xtables.lock && \
    chmod 0666 /run/xtables.lock
COPY --from=build /out/linkerd2-proxy-init /usr/local/bin/proxy-init
RUN setcap cap_net_raw,cap_net_admin+eip /sbin/xtables-legacy-multi && \
    setcap cap_net_raw,cap_net_admin+eip /sbin/xtables-nft-multi && \
    setcap cap_net_raw,cap_net_admin+eip /usr/local/bin/proxy-init
USER 65534
ENTRYPOINT ["/usr/local/bin/proxy-init"]
