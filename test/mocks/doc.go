// Package mocks holds gomock mocks generated from the interfaces in internal/port.
// Regenerate them with "go generate ./test/mocks/...".
package mocks

//go:generate go run go.uber.org/mock/mockgen@v0.6.0 -source=../../internal/port/cache.go -destination=mock_cache.go -package=mocks
//go:generate go run go.uber.org/mock/mockgen@v0.6.0 -source=../../internal/port/repository.go -destination=mock_repository.go -package=mocks
//go:generate go run go.uber.org/mock/mockgen@v0.6.0 -source=../../internal/port/service.go -destination=mock_service.go -package=mocks
//go:generate go run go.uber.org/mock/mockgen@v0.6.0 -source=../../internal/port/token.go -destination=mock_token.go -package=mocks
//go:generate go run go.uber.org/mock/mockgen@v0.6.0 -source=../../internal/port/tx.go -destination=mock_tx.go -package=mocks
