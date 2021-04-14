## compile proxy-init utility
FROM --platform=$BUILDPLATFORM golang:1.16.2 as golang
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
FROM --platform=$TARGETPLATFORM alpine:20210212
RUN apk add iptables
COPY LICENSE /linkerd/LICENSE
COPY --from=golang /out/linkerd2-proxy-init /usr/local/bin/proxy-init
ENTRYPOINT ["/usr/local/bin/proxy-init"]
