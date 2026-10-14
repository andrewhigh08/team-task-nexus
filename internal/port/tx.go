package port

import "context"

// TxManager runs a function inside a database transaction.
type TxManager interface {
	// WithinTx runs fn inside a transaction carried in the context it passes to fn, where the
	// repositories pick it up. The transaction commits when fn returns nil and rolls back when
	// it fails or panics. A call nested in another joins the outer transaction.
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}
