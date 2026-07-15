// Module path is a bare name (not a URL) on purpose: mandos is an INTERNAL tool,
// consumed only as a shelled-out CLI. Nothing ever `go get`s it, so this path is
// never sent to proxy.golang.org / sum.golang.org and can't be indexed by pkg.go.dev.
module mandos

go 1.26

require gopkg.in/yaml.v3 v3.0.1
