## compile proxy-init utility
FROM gcr.io/linkerd-io/go-deps:f364cab7 as golang
WORKDIR /go/src/github.com/linkerd/linkerd2-proxy-init
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go install -v

## package runtime
FROM gcr.io/linkerd-io/base:2019-02-19.01
COPY LICENSE /linkerd/LICENSE
COPY --from=golang /go/bin/linkerd2-proxy-init /usr/local/bin/proxy-init
ENTRYPOINT ["/usr/local/bin/proxy-init"]
