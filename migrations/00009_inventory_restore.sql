-- +goose Up
ALTER TABLE inventory.reservations DROP CONSTRAINT chk_reservations_status;
ALTER TABLE inventory.reservations ADD CONSTRAINT chk_reservations_status
    CHECK (status IN ('held', 'committed', 'released', 'expired', 'restored'));

-- +goose Down
UPDATE inventory.reservations SET status = 'committed' WHERE status = 'restored';
ALTER TABLE inventory.reservations DROP CONSTRAINT chk_reservations_status;
ALTER TABLE inventory.reservations ADD CONSTRAINT chk_reservations_status
    CHECK (status IN ('held', 'committed', 'released', 'expired'));
