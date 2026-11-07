package workflows

import (
	"context"

	"github.com/google/uuid"

	"github.com/cloudster1/rotor/backend/internal/db"
)

// Binder is a workflow whose current version binds a tool by name, for an administrator renaming that tool (#267).
type Binder struct {
	ID        uuid.UUID
	Name      string
	OwnerName string
}

// Binding are the workflows not archived whose current version binds the tool name, read as the control plane: an
// administrator renaming a tool must hear of every workflow that stops finding it, not only those they may open.
func (s *Service) Binding(ctx context.Context, tool string) ([]Binder, error) {
	out := []Binder{}
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT w.id, w.name, w.owner_name FROM workflow w
			JOIN workflow_version v ON v.workflow_id = w.id AND v.number = w.current_version
			WHERE w.archived_at IS NULL AND EXISTS (
				SELECT 1 FROM jsonb_each(COALESCE(v.spec -> 'tools', '{}'::jsonb)) b(name, binding)
				WHERE b.binding ->> 'tool' = $1)
			ORDER BY lower(w.name), w.id`, tool)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var b Binder
			if err := rows.Scan(&b.ID, &b.Name, &b.OwnerName); err != nil {
				return err
			}
			out = append(out, b)
		}
		return rows.Err()
	})
	return out, err
}
