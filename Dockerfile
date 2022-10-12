##
## Go
##

# Cross compile from native platform to target arch
FROM --platform=$BUILDPLATFORM golang:1.19.2-alpine as go
WORKDIR /build
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY ./proxy-init ./proxy-init
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -o /out/linkerd2-proxy-init -mod=readonly -ldflags "-s -w" -v ./proxy-init

##
## Rust
##

# Compile from target platform to target arch
FROM --platform=$TARGETPLATFORM docker.io/library/rust:1.64.0-slim as rust
WORKDIR /build
COPY Cargo.toml Cargo.lock .
COPY validator /build/
ARG TARGETARCH
RUN --mount=type=cache,target=target \
   --mount=type=cache,from=docker.io/library/rust:1.63.0-slim,source=/usr/local/cargo,target=/usr/local/cargo \
   cargo fetch
RUN --mount=type=cache,target=target \
   --mount=type=cache,from=docker.io/library/rust:1.63.0-slim,source=/usr/local/cargo,target=/usr/local/cargo \
   target=$(rustup show | sed -n 's/^Default host: \(.*\)/\1/p' | sed 's/-gnu$/-musl/') ; \
   rustup target add "${target}" && \
   cargo build --locked --target="$target" --release --package=linkerd-network-validator && \
   mv "target/${target}/release/linkerd-network-validator" /tmp/

##
## Runtime
## 

FROM --platform=$TARGETPLATFORM alpine:3.16.2 as runtime
RUN apk add iptables libcap && \
    touch /run/xtables.lock && \
    chmod 0666 /run/xtables.lock

# Copy proxy-init and validator binaries in the bin directory
COPY --from=go /out/linkerd2-proxy-init /usr/local/bin/proxy-init
COPY --from=rust /tmp/linkerd-network-validator /usr/local/bin/

# Set sys caps for iptables utilities and proxy-init
RUN setcap cap_net_raw,cap_net_admin+eip /sbin/xtables-legacy-multi && \
    setcap cap_net_raw,cap_net_admin+eip /sbin/xtables-nft-multi && \
    setcap cap_net_raw,cap_net_admin+eip /usr/local/bin/proxy-init

USER 65534
ENTRYPOINT ["/usr/local/bin/proxy-init"]
