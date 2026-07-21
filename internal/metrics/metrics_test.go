package metrics

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStatusClass(t *testing.T) {
	tests := []struct {
		give int
		want string
	}{
		{give: 200, want: "2xx"},
		{give: 202, want: "2xx"},
		{give: 400, want: "4xx"},
		{give: 404, want: "4xx"},
		{give: 413, want: "4xx"},
		{give: 500, want: "5xx"},
		{give: 503, want: "5xx"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, StatusClass(tt.give))
		})
	}
}

func TestRegistry_HasProcessAndGoCollectors(t *testing.T) {
	r := newRegistry()
	families, err := r.Gather()
	assert.NoError(t, err)
	assert.NotEmpty(t, families, "registry should expose at least the process/go collectors")
}
