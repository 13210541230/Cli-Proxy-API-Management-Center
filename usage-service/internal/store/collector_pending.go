package store

import (
	"context"
	"time"
)

const (
	collectorPendingStatusPending      = "pending"
	collectorPendingStatusDeadLettered = "dead_lettered"
	defaultCollectorPendingBatch       = 1000
)

type CollectorPendingItem struct {
	ID          int64
	Payload     string
	Status      string
	CreatedAtMS int64
}

// EnqueueCollectorPendingItems durably records a destructively popped queue
// batch before parsing/insertion. The collector can replay these items after a
// process crash or a SQLite/insert failure.
func (s *Store) EnqueueCollectorPendingItems(ctx context.Context, payloads []string) error {
	if len(payloads) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx, `insert into collector_pending_items(payload, status, created_at_ms) values(?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	createdAt := time.Now().UnixMilli()
	for _, payload := range payloads {
		if _, err := stmt.ExecContext(ctx, payload, collectorPendingStatusPending, createdAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) LoadCollectorPendingItems(ctx context.Context, limit int) ([]CollectorPendingItem, error) {
	if limit <= 0 {
		limit = defaultCollectorPendingBatch
	}
	rows, err := s.db.QueryContext(ctx, `select id, payload, status, created_at_ms
		from collector_pending_items order by id asc limit ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]CollectorPendingItem, 0, limit)
	for rows.Next() {
		var item CollectorPendingItem
		if err := rows.Scan(&item.ID, &item.Payload, &item.Status, &item.CreatedAtMS); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) MarkCollectorPendingDeadLettered(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `update collector_pending_items set status = ? where id = ?`, collectorPendingStatusDeadLettered, id)
	return err
}

func (s *Store) DeleteCollectorPendingItems(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx, `delete from collector_pending_items where id = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, id := range ids {
		if _, err := stmt.ExecContext(ctx, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) CollectorPendingItemCount(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, `select count(*) from collector_pending_items`).Scan(&count)
	return count, err
}

func IsCollectorPendingDeadLettered(item CollectorPendingItem) bool {
	return item.Status == collectorPendingStatusDeadLettered
}
