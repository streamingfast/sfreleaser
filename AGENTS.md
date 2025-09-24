This project is a CLI application in Golang that wraps Goreleaser tool
as well as leverage `gh` CLI tool to automate the release process of a Go & Rust projects
with maintained at StreamingFast.

It runs actually Goreleaser cross via a Docker image, so the build is reproducible.

Always run `go test ./...` and `gofmt` at the end of a Golang code generation session to ensure
test passes and code is formatted properly.

When running command in this project to test the `./cmd/sfreleaser` binary, always use `./devel/sfreleaser`
script which is proxy to `./cmd/sfreleaser` but compiles it before running it.

NEVER run the command `go build -o devel/sfreleaser ./cmd/sfreleaser`, this would overwrite our `devel/sfreleaser`
script we use for development.