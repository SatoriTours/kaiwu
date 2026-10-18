package workbench

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// The integration queue is a persisted FIFO ticket per delivery lane. A lane
// is keyed twice: by the canonical common workspace directory (one writer per
// clone) and by the repository main branch (one writer per remote main across
// clones and project entries). Tickets are never reordered, a cycle keeps its
// ticket across restarts, and only a verified or explicitly cancelled cycle
// releases the quarantine.

// deliveryLaneKeys derives the two lane keys from the frozen cycle. The
// common directory must be known: without it no writer isolation can be
// proven, so the cycle is refused instead of guessed.
func deliveryLaneKeys(d Delivery) (common, remote string, err error) {
	common = strings.TrimSpace(d.CommonDir)
	if common == "" {
		return "", "", Invalid("交付周期缺少主工作区共用目录，不能进入集成队列")
	}
	common = filepath.ToSlash(filepath.Clean(common))
	r := d.Policy.Repository
	remote = fmt.Sprintf("%d|%s|%s", r.ID, strings.ToLower(r.Owner+"/"+r.Name), strings.ToLower(r.MainBranch))
	return common, remote, nil
}

// EnqueueDelivery records one waiting ticket for an active cycle and returns
// its stable FIFO number. Enqueueing an already queued cycle returns the
// existing ticket instead of minting another place in line.
func (s *Store) EnqueueDelivery(deliveryID string) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, errors.New("store closed")
	}
	d, err := s.deliveryLocked(deliveryID)
	if err != nil {
		return 0, err
	}
	if !DeliveryActive(d.Phase) {
		return 0, Conflict("交付周期已结束，不能进入队列")
	}
	common, remote, err := deliveryLaneKeys(d)
	if err != nil {
		return 0, err
	}
	var ticket uint64
	err = s.db.QueryRow("SELECT ticket FROM delivery_queue WHERE delivery_id=?", deliveryID).Scan(&ticket)
	if err == nil {
		return ticket, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	next := clone(s.state)
	next.Revision++
	err = s.persistWith(next, "", "", Receipt{}, func(tx *sql.Tx) error {
		r, e := tx.Exec("INSERT INTO delivery_queue(delivery_id,common_key,remote_key,owner) VALUES(?,?,?,0)", deliveryID, common, remote)
		if e != nil {
			return e
		}
		inserted, e := r.LastInsertId()
		if e != nil {
			return e
		}
		ticket = uint64(inserted)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return ticket, nil
}

// AcquireDeliveryTurn grants the lane to exactly the oldest waiting ticket
// that shares either of its two keys. Until then it reports false without
// touching the queue; a cycle that already owns its turn stays granted.
func (s *Store) AcquireDeliveryTurn(deliveryID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false, errors.New("store closed")
	}
	d, err := s.deliveryLocked(deliveryID)
	if err != nil {
		return false, err
	}
	common, remote, err := deliveryLaneKeys(d)
	if err != nil {
		return false, err
	}
	var ticket uint64
	var owner int
	err = s.db.QueryRow("SELECT ticket, owner FROM delivery_queue WHERE delivery_id=?", deliveryID).Scan(&ticket, &owner)
	if errors.Is(err, sql.ErrNoRows) {
		return false, Missing()
	}
	if err != nil {
		return false, err
	}
	if owner == 1 {
		return true, nil
	}
	if !DeliveryActive(d.Phase) {
		return false, Conflict("交付周期已结束，不能取得集成轮次")
	}
	// FIFO is per lane: any ticket ahead of this one on the same clone or the
	// same remote main keeps the turn, whether it is still waiting or already
	// integrating.
	var ahead int
	err = s.db.QueryRow("SELECT COUNT(*) FROM delivery_queue WHERE ticket < ? AND (common_key=? OR remote_key=?)", ticket, common, remote).Scan(&ahead)
	if err != nil {
		return false, err
	}
	if ahead > 0 {
		return false, nil
	}
	next := clone(s.state)
	err = s.persistWith(next, "", "", Receipt{}, func(tx *sql.Tx) error {
		r, e := tx.Exec("UPDATE delivery_queue SET owner=1 WHERE delivery_id=? AND owner=0", deliveryID)
		if e != nil {
			return e
		}
		n, e := r.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return Conflict("交付队列槽已被占用")
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

// ReleaseDeliveryTurn drops the waiting ticket and frees both lanes. A cycle
// that has not passed verification may only be released when the caller
// explicitly admits the stop happened unverified; that admission is persisted
// as a warning step so the missing proof stays visible. A cycle that never
// entered the queue is a defensive no-op.
func (s *Store) ReleaseDeliveryTurn(deliveryID string, allowUnverified bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("store closed")
	}
	d, err := s.deliveryLocked(deliveryID)
	if err != nil {
		return err
	}
	var ticket uint64
	err = s.db.QueryRow("SELECT ticket FROM delivery_queue WHERE delivery_id=?", deliveryID).Scan(&ticket)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	verified := d.Phase == "succeeded"
	if !verified && !allowUnverified {
		return Conflict("交付周期未完成核验，隔离必须保留；如确认停止请明确告知未验证")
	}
	var step *DeliveryStep
	if !verified {
		step = &DeliveryStep{
			ID:               id(),
			DeliveryID:       d.ID,
			Kind:             "release_unverified",
			InputFingerprint: "release:" + d.Phase,
			Phase:            "observed",
			Summary:          "显式解除交付隔离：周期未完成核验，未验证警告保留",
			ExitKnown:        true,
		}
	}
	writeStep, prepared, err := s.prepareDeliveryStep(deliveryID, step)
	if err != nil {
		return err
	}
	summary, err := s.deliverySummary(d, prepared, nil)
	if err != nil {
		return err
	}
	next := clone(s.state)
	next.Deliveries[d.TaskID] = summary
	return s.persistWith(next, "", "", Receipt{}, func(tx *sql.Tx) error {
		if writeStep != nil {
			if e := writeStep(tx); e != nil {
				return e
			}
		}
		_, e := tx.Exec("DELETE FROM delivery_queue WHERE delivery_id=?", deliveryID)
		return e
	})
}
