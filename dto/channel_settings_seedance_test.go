package dto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSeedanceV3RoutesValidate(t *testing.T) {
	t.Run("accepts GET path template and POST body query", func(t *testing.T) {
		for _, route := range []*SeedanceV3Route{
			{Method: "GET", Target: "/tasks/{task_id}"},
			{Method: "POST", Target: "https://query.example.com/tasks"},
		} {
			err := (&SeedanceV3Routes{TaskGet: route}).Validate()
			require.NoError(t, err)
		}
	})

	t.Run("accepts GET query parameter template", func(t *testing.T) {
		err := (&SeedanceV3Routes{
			TaskGet: &SeedanceV3Route{
				Method:     "GET",
				Target:     "/tasks/query",
				Parameters: map[string]any{"job_id": "{task_id}"},
			},
		}).Validate()
		require.NoError(t, err)
	})

	t.Run("rejects GET query without task placeholder", func(t *testing.T) {
		err := (&SeedanceV3Routes{
			TaskGet: &SeedanceV3Route{Method: "GET", Target: "/tasks/query"},
		}).Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "{task_id}")
	})
}
