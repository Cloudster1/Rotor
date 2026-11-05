-- +goose Up
-- Author has created skills since 01410 (#219), so its description says so (#236). An administrator may have reworded
-- it since, so only the words it shipped with are replaced.
UPDATE role SET description = 'Creates workflows and skills of their own.'
WHERE key = 'author' AND description = 'Creates workflows of their own.';

-- +goose Down
UPDATE role SET description = 'Creates workflows of their own.'
WHERE key = 'author' AND description = 'Creates workflows and skills of their own.';
