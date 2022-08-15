ARG RUST_IMAGE=docker.io/library/rust:1.62.1

FROM $RUST_IMAGE as build
ARG TARGETARCH
WORKDIR /build
COPY Cargo.toml Cargo.lock .
COPY validator /build/
RUN --mount=type=cache,target=target \
    --mount=type=cache,from=rust:1.62.1,source=/usr/local/cargo,target=/usr/local/cargo \
    cargo fetch
RUN --mount=type=cache,target=target \
    --mount=type=cache,from=rust:1.62.1,source=/usr/local/cargo,target=/usr/local/cargo \
    target=$(rustup show | sed -n 's/^Default host: \(.*\)/\1/p' | sed 's/-gnu$/-musl/') ; \
    rustup target add "${target}" && \
    cargo build --locked --target="$target" --release --package=validator && \
    mv "target/${target}/release/validator" /tmp/

FROM scratch
COPY --from=build /tmp/validator /bin/
ENTRYPOINT ["/bin/validator"]
