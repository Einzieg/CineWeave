package production

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

type staleRecordingExecer struct {
	args [][]any
}

func (r *staleRecordingExecer) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	r.args = append(r.args, args)
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func TestAssetStalePropagationDistinguishesIdentityAndProductionMaterial(t *testing.T) {
	tests := []struct {
		name      string
		mark      func(context.Context, Execer, string, string) error
		wantState string
	}{
		{name: "identity change", mark: MarkAssetDownstreamStale, wantState: "upstream_changed"},
		{name: "prompt or reference generation", mark: MarkAssetProductionMaterialStale, wantState: "needs_regeneration"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := &staleRecordingExecer{}
			if err := test.mark(context.Background(), recorder, "project-id", "asset-id"); err != nil {
				t.Fatalf("mark stale: %v", err)
			}
			if len(recorder.args) != 2 {
				t.Fatalf("exec calls = %d, want 2", len(recorder.args))
			}
			if len(recorder.args[0]) != 3 || recorder.args[0][2] != test.wantState {
				t.Fatalf("requirement stale args = %#v, want state %q", recorder.args[0], test.wantState)
			}
		})
	}
}
