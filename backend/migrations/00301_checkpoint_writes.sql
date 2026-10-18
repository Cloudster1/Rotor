-- +goose Up
-- LangGraph stores a step's pending writes while the checkpoint they belong to may still be on its way, so a write
-- names its checkpoint without waiting for it; the run alone holds it, and deleting the run deletes both.
ALTER TABLE run_checkpoint_write DROP CONSTRAINT run_checkpoint_write_run_id_checkpoint_ns_checkpoint_id_fkey;
ALTER TABLE run_checkpoint_write ADD CONSTRAINT run_checkpoint_write_run_fkey FOREIGN KEY (run_id) REFERENCES run(id) ON DELETE CASCADE;

-- +goose Down
ALTER TABLE run_checkpoint_write DROP CONSTRAINT IF EXISTS run_checkpoint_write_run_fkey;
ALTER TABLE run_checkpoint_write ADD CONSTRAINT run_checkpoint_write_run_id_checkpoint_ns_checkpoint_id_fkey
    FOREIGN KEY (run_id, checkpoint_ns, checkpoint_id) REFERENCES run_checkpoint (run_id, checkpoint_ns, checkpoint_id) ON DELETE CASCADE;
