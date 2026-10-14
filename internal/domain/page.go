package domain

// PageRequest selects page number Page, counted from 1, of a list cut into pages of PageSize
// items.
type PageRequest struct {
	Page     int
	PageSize int
}
