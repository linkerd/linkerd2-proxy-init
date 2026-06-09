
# go-build all packages into target binaries
go-build: \
    (go-build-pkg "linkerd2-cni-plugin" "cni-plugin") \
    (go-build-pkg "linkerd2-proxy-init" "proxy-init")

# go-fmt-check the root directory for all fmt errors printing the result;
# and exiting non-zero if formatting issues exist.
go-fmt-check:
    out=$(gofmt -e -d .) ; [ -z "$out" ] || (echo "$out" ; exit 1)

# go-lint all packages
go-lint *flags: \
    (go-lint-pkg "cni-plugin" flags) \
    (go-lint-pkg "pkg" flags) \
    (go-lint-pkg "proxy-init" flags)

# go-test all packages w/ gotestsum and write results to ${pkg}-test.json
#
# cni-plugin is not tested here as it only include integration tests
go-test: \
    (go-test-pkg "pkg") \
    (go-test-pkg "proxy-init")

# go-build runs go build against a specific package outputting to a specific
# target binary (no optional flags)
go-build-pkg out pkg:
    go build -o target/{{ out }} ./{{ pkg }}

# go-lint-pkg runs go lint against a specific package with optional flags
go-lint-pkg pkg *flags:
    golangci-lint run ./{{ pkg }}/... {{ flags }}

# go-test runs gotestsum against a specific package with optional flags writing
# the output/result to ${pkg}-test.json and coverage detail to ${pkg}-cover.out
go-test-pkg pkg *flags:
    gotestsum --jsonfile {{ pkg }}-test.json -- \
        -coverprofile {{ pkg }}-cover.out \
        -race -v -mod=readonly -v ./{{ pkg }}/... {{ flags }}
