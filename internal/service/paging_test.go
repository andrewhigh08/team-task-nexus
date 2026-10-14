package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/andrewhigh08/team-task-nexus/internal/domain"
)

// invalidPages are the page requests every paged list rejects, with the problems it reports.
var invalidPages = []struct {
	name string
	p    domain.PageRequest
	want map[string]string
}{
	{name: "page size above the maximum", p: domain.PageRequest{PageSize: 101}, want: map[string]string{"page_size": "must be between 1 and 100"}},
	{name: "page above the maximum", p: domain.PageRequest{Page: 10_001}, want: map[string]string{"page": "must be between 1 and 10000"}},
	{
		name: "negative numbers",
		p:    domain.PageRequest{Page: -1, PageSize: -2},
		want: map[string]string{"page": "must be between 1 and 10000", "page_size": "must be between 1 and 100"},
	},
}

func TestNormalizePageRequest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		p    domain.PageRequest
		want domain.PageRequest
	}{
		{name: "defaults", p: domain.PageRequest{}, want: domain.PageRequest{Page: 1, PageSize: 20}},
		{name: "default size only", p: domain.PageRequest{Page: 4}, want: domain.PageRequest{Page: 4, PageSize: 20}},
		{name: "smallest", p: domain.PageRequest{Page: 1, PageSize: 1}, want: domain.PageRequest{Page: 1, PageSize: 1}},
		{name: "largest", p: domain.PageRequest{Page: 10_000, PageSize: 100}, want: domain.PageRequest{Page: 10_000, PageSize: 100}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := tt.p

			require.NoError(t, normalizePageRequest(&p))
			assert.Equal(t, tt.want, p)
		})
	}
	for _, tt := range invalidPages {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := tt.p

			requireFieldErrors(t, normalizePageRequest(&p), tt.want)
		})
	}
}

func TestTotalPages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		total, pageSize, want int
	}{
		{total: 0, pageSize: 20, want: 0},
		{total: 1, pageSize: 20, want: 1},
		{total: 20, pageSize: 20, want: 1},
		{total: 21, pageSize: 20, want: 2},
		{total: 41, pageSize: 1, want: 41},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, totalPages(tt.total, tt.pageSize), "%d items in pages of %d", tt.total, tt.pageSize)
	}
}
