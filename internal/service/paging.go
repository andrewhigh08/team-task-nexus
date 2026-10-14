package service

import (
	"fmt"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
)

// Paging of the task list, the task history and the comments. maxPage keeps the OFFSET the
// repositories compute far from overflow; deeper pages of the task list are better reached by
// narrowing the filter.
const (
	defaultPageSize = 20
	maxPageSize     = 100
	maxPage         = 10_000
)

// normalizePage fills in the default page number and page size, then records in errs either
// one that is out of range.
func normalizePage(page, pageSize *int, errs fieldErrors) {
	if *page == 0 {
		*page = 1
	}
	if *pageSize == 0 {
		*pageSize = defaultPageSize
	}
	if *page < 1 || *page > maxPage {
		errs.add("page", fmt.Sprintf("must be between 1 and %d", maxPage))
	}
	if *pageSize < 1 || *pageSize > maxPageSize {
		errs.add("page_size", fmt.Sprintf("must be between 1 and %d", maxPageSize))
	}
}

// normalizePageRequest fills in the default page number and page size of p and validates it.
func normalizePageRequest(p *domain.PageRequest) error {
	errs := fieldErrors{}
	normalizePage(&p.Page, &p.PageSize, errs)
	return errs.err()
}

// totalPages returns the number of pages of pageSize items that total items fill.
func totalPages(total, pageSize int) int {
	return (total + pageSize - 1) / pageSize
}
