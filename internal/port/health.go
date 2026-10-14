package port

import "context"

// Pinger checks that a dependency the service cannot work without answers. The readiness
// probe pings every such dependency.
type Pinger interface {
	// Ping returns an error when the dependency does not answer within ctx.
	Ping(ctx context.Context) error
}
